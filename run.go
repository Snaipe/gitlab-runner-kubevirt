// Copyright 2023, Franklin "Snaipe" Mathieu <me@snai.pe>
//
// Use of this source-code is govered by the MIT license, which
// can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path"
	"strings"
	"time"

	"barney.ci/shutil"
	"github.com/cenkalti/backoff/v4"
	"github.com/helloyi/go-sshclient"
	"golang.org/x/crypto/ssh"
	"golang.org/x/text/encoding/unicode"
	kubevirt "kubevirt.io/client-go/kubecli"
)

type SSHConfig struct {
	Port     string `name:"port" default:"22" help:"Port to ssh to"`
	User     string `name:"user" help:"ssh username"`
	Password string `name:"password" xor:"auth" help:"ssh password"`
	PrivKey  string `name:"private-key-file" xor:"auth" help:"ssh private key"`
}

type RunConfig struct {
	Shell  string    `name:"shell" required enum:"bash,pwsh" help:"shell to use when executing script"`
	Method string    `name:"method" default:"ssh" enum:"ssh" help:"method to execute script"`
	SSH    SSHConfig `embed prefix:"ssh-" group:"SSH method options:"`
}

const RunConfigKey = labelPrefix + "/runconfig"

type RunCmd struct {
	Script string `arg`
	Stage  string `arg`

	RetryTimeout time.Duration `default:"5m"`
	DialTimeout  time.Duration `default:"10s"`
}

func (cmd *RunCmd) Run(ctx context.Context, client kubevirt.KubevirtClient, jctx *JobContext) error {

	vm, err := FindJobVM(ctx, client, jctx)
	if err != nil {
		return err
	}

	var rc RunConfig
	if err := json.Unmarshal([]byte(vm.Annotations[RunConfigKey]), &rc); err != nil {
		return err
	}

	if vm.Status.Phase != "Running" {
		return fmt.Errorf("Virtual Machine instance %s is not running (phase: %v)", vm.ObjectMeta.Name, vm.Status.Phase)
	}

	// Wait for VM to have an IP (handles guest agent transition)
	timeout, stop := context.WithTimeout(ctx, cmd.RetryTimeout)
	defer stop()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	fmt.Fprintf(Debug, "Waiting for Virtual Machine instance %s to have an IP address...\n", vm.ObjectMeta.Name)

	for len(vm.Status.Interfaces) == 0 || vm.Status.Interfaces[0].IP == "" {
		select {
		case <-timeout.Done():
			return fmt.Errorf("Virtual Machine instance %s has no IP after timeout; is it running?", vm.ObjectMeta.Name)
		case <-ticker.C:
			vm, err = FindJobVM(timeout, client, jctx)
			if err != nil {
				return err
			}
			if len(vm.Status.Interfaces) > 0 && vm.Status.Interfaces[0].IP != "" {
				break
			}
			fmt.Fprintf(Debug, "VM %s still has no IP, retrying...\n", vm.ObjectMeta.Name)
		}
	}

	ip := vm.Status.Interfaces[0].IP
	fmt.Fprintf(Debug, "Virtual Machine instance has IP: %s\n", ip)

	switch rc.Method {
	case "ssh":
		client, err := DialSSH(timeout, ip, rc.SSH, cmd.DialTimeout)
		if err != nil {
			return err
		}
		defer client.Close()

		ext := rc.Shell
		switch rc.Shell {
		case "pwsh":
			ext = "ps1"
		}

		scriptPath := path.Join(cmd.Stage + "." + ext)

		fmt.Fprintf(Debug, "uploading script %v\n", cmd.Script)
		if err := client.Sftp().Upload(cmd.Script, scriptPath); err != nil {
			return err
		}

		if cli.Debug {
			contents, err := os.ReadFile(cmd.Script)
			fmt.Fprintf(Debug, "contents of %v:\n", cmd.Script)
			if err == nil {
				Debug.Write(contents)
			} else {
				fmt.Fprintf(Debug, "<ERROR: %v>", err)
			}
			fmt.Fprintf(Debug, "---\n", cmd.Script)
		}

		argv := generateShellArgv(rc.Shell, scriptPath)

		fmt.Fprintf(Debug, "executing %v\n", argv)
		if err := client.Cmd(shutil.Quote(argv)).SetStdio(os.Stdout, os.Stderr).Run(); err != nil {
			var exiterr *ssh.ExitError
			if errors.As(err, &exiterr) {
				switch {
				case exiterr.Signal() != "":
					fmt.Fprintf(os.Stderr, "Command crashed with signal %v\n", exiterr.Signal())
				case exiterr.ExitStatus() != 0:
					fmt.Fprintf(os.Stderr, "Command exited with status %v\n", exiterr.ExitStatus())
				default:
					fmt.Fprintf(os.Stderr, "Command exited with message %q\n", exiterr.Msg())
				}
				buildFailureExit()
			}
			return err
		}
	default:
		panic("unknown run method")
	}

	return nil
}

func generateShellArgv(shell, script string) []string {
	switch shell {
	case "bash":
		return []string{"bash", script}
	case "pwsh":
		// See https://gitlab.com/gitlab-org/gitlab-runner/-/blob/d5e1f7b0adb2b54d136155e3bc3ef3e5ff74d217/shells/powershell.go#L89-126
		// for an explanation of why the base64+utf16 encoding is necessary.

		encoder := unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewEncoder()

		var sb strings.Builder
		sb.WriteString("$OutputEncoding = [console]::InputEncoding = [console]::OutputEncoding = New-Object System.Text.UTF8Encoding\r\n")
		sb.WriteString(shell + " " + script + "\r\n")
		sb.WriteString("exit $LASTEXITCODE\r\n")
		encoded, _ := encoder.String(sb.String())

		return []string{
			"pwsh",
			"-NoProfile",
			"-NoLogo",
			"-InputFormat",
			"text",
			"-OutputFormat",
			"text",
			"-NonInteractive",
			"-ExecutionPolicy",
			"Bypass",
			"-EncodedCommand",
			base64.StdEncoding.EncodeToString([]byte(encoded)),
		}
	default:
		panic("unsupported shell")
	}
}

func DialSSH(ctx context.Context, ip string, config SSHConfig, dialTimeout time.Duration) (client *sshclient.Client, err error) {

	back := backoff.NewExponentialBackOff()
	back.MaxInterval = 5 * time.Second

	for {
		fmt.Fprintf(Debug, "attempting to connect to %s:%s...\n", ip, config.Port)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		sshconfig := ssh.ClientConfig{
			User:            config.User,
			Timeout:         dialTimeout,
			HostKeyCallback: ssh.HostKeyCallback(func(hostname string, remote net.Addr, key ssh.PublicKey) error { return nil }),
		}

		if config.PrivKey != "" {
			key, err := os.ReadFile(config.PrivKey)
			if err != nil {
				return nil, err
			}

			signer, err := ssh.ParsePrivateKey(key)
			if err != nil {
				return nil, err
			}

			sshconfig.Auth = append(sshconfig.Auth, ssh.PublicKeys(signer))
		}

		sshconfig.Auth = append(sshconfig.Auth, ssh.Password(config.Password))

		client, err = sshclient.Dial("tcp", ip+":"+config.Port, &sshconfig)
		var netErr *net.OpError
		switch {
		case errors.As(err, &netErr) && netErr.Op == "dial":
			fmt.Fprintln(Debug, err)
			time.Sleep(back.NextBackOff())
			continue
		case err != nil:
			return nil, err
		}
		return client, nil
	}
}
