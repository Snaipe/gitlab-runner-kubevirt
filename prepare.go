// Copyright 2023, Franklin "Snaipe" Mathieu <me@snai.pe>
//
// Use of this source-code is govered by the MIT license, which
// can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/watch"
	kubevirtapi "kubevirt.io/api/core/v1"
	kubevirt "kubevirt.io/client-go/kubecli"
)

type PrepareCmd struct {
	DefaultImage                   string        `name:"default-image"`
	DefaultImagePullPolicy         string        `name:"default-image-pull-policy"`
	DefaultImagePullSecret         string        `name:"default-image-pull-secret"`
	DefaultCPURequest              string        `name:"default-cpu-request" default:"1"`
	DefaultCPULimit                string        `name:"default-cpu-limit" default:"1"`
	DefaultMemoryRequest           string        `name:"default-memory-request" default:"1Gi"`
	DefaultMemoryLimit             string        `name:"default-memory-limit" default:"1Gi"`
	DefaultEphemeralStorageRequest string        `name:"default-ephemeral-storage-request"`
	DefaultEphemeralStorageLimit   string        `name:"default-ephemeral-storage-limit"`
	DefaultTimezone                string        `name:"default-timezone" default:"Etc/UTC" env:"CUSTOM_ENV_VM_TIMEZONE"`
	Timeout                        time.Duration `name:"timeout" default:"1h"`
	DialTimeout                    time.Duration `default:"10s"`

	RunConfig `embed`
}

func (cmd *PrepareCmd) Run(ctx context.Context, client kubevirt.KubevirtClient, jctx *JobContext) error {
	if jctx.CPURequest == "" {
		jctx.CPURequest = cmd.DefaultCPURequest
	}
	if jctx.CPULimit == "" {
		jctx.CPULimit = cmd.DefaultCPULimit
	}
	if jctx.MemoryRequest == "" {
		jctx.MemoryRequest = cmd.DefaultMemoryRequest
	}
	if jctx.MemoryLimit == "" {
		jctx.MemoryLimit = cmd.DefaultMemoryLimit
	}
	if jctx.EphemeralStorageRequest == "" {
		jctx.EphemeralStorageRequest = cmd.DefaultEphemeralStorageRequest
	}
	if jctx.EphemeralStorageLimit == "" {
		jctx.EphemeralStorageLimit = cmd.DefaultEphemeralStorageLimit
	}
	if jctx.ImagePullPolicy == "" {
		jctx.ImagePullPolicy = cmd.DefaultImagePullPolicy
	}
	if jctx.ImagePullSecret == "" {
		jctx.ImagePullSecret = cmd.DefaultImagePullSecret
	}
	if jctx.Image == "" {
		jctx.Image = cmd.DefaultImage
	}
	if jctx.Timezone == "" {
		jctx.Timezone = cmd.DefaultTimezone
	}

	rc := cmd.RunConfig

	fmt.Fprintf(os.Stderr, "Creating Virtual Machine instance\n")

	vm, err := CreateJobVM(ctx, client, jctx, &rc)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Waiting for Virtual Machine instance %s to be ready...\n", vm.ObjectMeta.Name)

	// Wait for new VM to get an IP

	timeout, stop := context.WithTimeout(ctx, cmd.Timeout)
	defer stop()

	err = WatchJobVM(timeout, client, jctx, vm, func(et watch.EventType, val *kubevirtapi.VirtualMachineInstance) error {
		if et == watch.Error {
			// Retry on watch failure
			return nil
		}
		vm = val
		if len(vm.Status.Interfaces) == 0 || vm.Status.Interfaces[0].IP == "" {
			return nil
		}
		for _, cond := range vm.Status.Conditions {
			if cond.Type == "Ready" && cond.Status == "True" {
				return ErrWatchDone
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "Virtual Machine instance is ready.")
	fmt.Fprintln(os.Stderr, "Name:", vm.ObjectMeta.Name)
	fmt.Fprintln(os.Stderr, "Image:", jctx.Image)
	fmt.Fprintln(os.Stderr, "Node:", vm.Status.NodeName)
	fmt.Fprintln(os.Stderr, "IP:", vm.Status.Interfaces[0].IP)

	fmt.Fprintln(os.Stderr, "Waiting for virtual machine to become reachable via ssh...")

	ssh, err := DialSSH(timeout, vm.Status.Interfaces[0].IP, rc.SSH, cmd.DialTimeout)
	if err != nil {
		return err
	}
	_ = ssh.Close()
	return nil
}
