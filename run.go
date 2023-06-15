// Copyright 2023, Franklin "Snaipe" Mathieu <me@snai.pe>
//
// Use of this source-code is govered by the MIT license, which
// can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/base64"
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

type RunCmd struct {
	Shell  string `name:"shell" required enum:"bash,pwsh" help:"shell to use when executing script"`
	Method string `name:"method" default:"ssh" enum:"ssh" help:"method to execute script"`

	SSH struct {
		IP       string `name:"ip" help:"IP to ssh to (for debugging)"`
		Port     string `name:"port" default:"22" help:"Port to ssh to"`
		User     string `name:"user" help:"ssh username"`
		Password string `name:"password" xor:"auth" help:"ssh password"`
		PrivKey  string `name:"private-key-file" xor:"auth" help:"ssh private key"`
	} `embed prefix:"ssh-" group:"SSH method options:"`

	Script string `arg`
	Stage  string `arg`
}

func (cmd *RunCmd) Run(ctx context.Context, client kubevirt.KubevirtClient, jctx *JobContext) error {

	switch cmd.Method {
	case "ssh":

		ip := cmd.SSH.IP
		if ip == "" {
			vm, err := FindJobVM(ctx, client, jctx)
			if err != nil {
				return err
			}

			if vm.Status.Phase != "Running" {
				return fmt.Errorf("Virtual Machine instance %s is not running (phase: %v)", vm.ObjectMeta.Name, vm.Status.Phase)
			}
			if len(vm.Status.Interfaces) == 0 || vm.Status.Interfaces[0].IP == "" {
				return fmt.Errorf("Virtual Machine instance %s has no IP; is it running?", vm.ObjectMeta.Name)
			}
			ip = vm.Status.Interfaces[0].IP
		}

		var (
			client *sshclient.Client
			err    error
		)

		retryDeadline := time.Now().Add(5 * time.Minute)
		back := backoff.NewExponentialBackOff()

		for time.Now().Before(retryDeadline) {
			switch {
			case cmd.SSH.PrivKey != "":
				client, err = sshclient.DialWithKey(ip+":"+cmd.SSH.Port, cmd.SSH.User, cmd.SSH.PrivKey)
			default:
				client, err = sshclient.DialWithPasswd(ip+":"+cmd.SSH.Port, cmd.SSH.User, cmd.SSH.Password)
			}
			var netErr *net.OpError
			switch {
			case errors.As(err, &netErr) && netErr.Op == "dial":
				fmt.Fprintln(Debug, err)
				time.Sleep(back.NextBackOff())
				continue
			case err != nil:
				return err
			}
			break
		}
		defer client.Close()

		ext := cmd.Shell
		switch cmd.Shell {
		case "pwsh":
			ext = "ps1"
		}

		scriptPath := path.Join(cmd.Stage + "." + ext)

		fmt.Fprintf(Debug, "uploading script %v\n", cmd.Script)
		if err := client.Sftp().Upload(cmd.Script, scriptPath); err != nil {
			return err
		}

		argv := generateShellArgv(cmd.Shell, scriptPath)

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
