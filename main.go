// Copyright 2023, Franklin "Snaipe" Mathieu <me@snai.pe>
//
// Use of this source-code is govered by the MIT license, which
// can be found in the LICENSE file.

package main

import (
	"context"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"hash"
	"io"
	"os"
	"strconv"

	"github.com/alecthomas/kong"
)

type JobContext struct {
	ID              string
	BaseName        string
	Image           string
	ImagePullPolicy string
	Namespace       string
	MachineType     string

	CPURequest              string
	CPULimit                string
	MemoryRequest           string
	MemoryLimit             string
	EphemeralStorageRequest string
	EphemeralStorageLimit   string
}

var cli struct {
	RunnerID     string `name:"runner-id" env:"CUSTOM_ENV_CI_RUNNER_ID"`
	ProjectID    string `name:"project-id" env:"CUSTOM_ENV_CI_PROJECT_ID"`
	ConcurrentID string `name:"concurrent-id" env:"CUSTOM_ENV_CI_CONCURRENT_PROJECT_ID"`
	JobID        string `name:"job-id" env:"CUSTOM_ENV_CI_JOB_ID"`
	JobImage     string `name:"image" env:"CUSTOM_ENV_CI_JOB_IMAGE"`
	Namespace    string `name:"namespace" env:"KUBEVIRT_NAMESPACE" default:"gitlab-runner"`
	Debug        bool

	Config  ConfigCmd  `cmd`
	Prepare PrepareCmd `cmd`
	Run     RunCmd     `cmd`
	Cleanup CleanupCmd `cmd`
}

var Debug io.Writer = io.Discard

func main() {

	ctx := kong.Parse(&cli)

	if cli.Debug {
		Debug = os.Stderr
	}

	jctx := contextFromEnv()

	ctx.Bind(jctx)
	ctx.BindToProvider(KubeClient)
	ctx.BindToProvider(func() (context.Context, error) {
		return context.Background(), nil
	})

	if err := ctx.Run(jctx); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", os.Args[0], err)
		systemFailureExit()
	}
}

func contextFromEnv() *JobContext {
	var jctx JobContext
	jctx.BaseName = fmt.Sprintf(`runner-%s-project-%s-concurrent-%s`, cli.RunnerID, cli.ProjectID, cli.ConcurrentID)
	jctx.ID = digest(sha1.New, cli.RunnerID, cli.ProjectID, cli.ConcurrentID, cli.JobID)
	jctx.Image = cli.JobImage
	jctx.Namespace = cli.Namespace
	return &jctx
}

func digest(hashfunc func() hash.Hash, v ...interface{}) string {
	digest := hashfunc()
	binary.Write(digest, binary.BigEndian, len(v))
	for _, e := range v {
		switch e := e.(type) {
		case string:
			binary.Write(digest, binary.BigEndian, len(e))
			io.WriteString(digest, e)
		case []byte:
			binary.Write(digest, binary.BigEndian, len(e))
			digest.Write(e)
		default:
			binary.Write(digest, binary.BigEndian, e)
		}
	}
	return fmt.Sprintf("%x", digest.Sum(nil))
}

func envExit(status int, env string) {
	if code := os.Getenv(env); code != "" {
		val, err := strconv.Atoi(code)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s=%s is not a valid exit code: %v\n", env, code, err)
		} else {
			status = val
		}
	}
	os.Exit(status)
}

func systemFailureExit() {
	envExit(2, "SYSTEM_FAILURE_EXIT_CODE")
}

func buildFailureExit() {
	envExit(1, "BUILD_FAILURE_EXIT_CODE")
}
