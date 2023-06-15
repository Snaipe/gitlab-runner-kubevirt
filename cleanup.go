// Copyright 2023, Franklin "Snaipe" Mathieu <me@snai.pe>
//
// Use of this source-code is govered by the MIT license, which
// can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	kubevirt "kubevirt.io/client-go/kubecli"
)

type CleanupCmd struct {
	Timeout time.Duration `name:"timeout" default:"1h"`
	SkipIf  []string      `name:"skip-if" sep:","`
}

func (cmd *CleanupCmd) Run(ctx context.Context, client kubevirt.KubevirtClient, jctx *JobContext) error {
	vm, err := FindJobVM(ctx, client, jctx)
	if err != nil {
		return err
	}

	for _, skipIf := range cmd.SkipIf {
		check := func() bool { return string(vm.Status.Phase) == skipIf }
		if strings.HasPrefix(skipIf, "!") {
			check = func() bool { return string(vm.Status.Phase) != skipIf[1:] }
		}
		if check() {
			fmt.Fprintf(os.Stderr, "Skipping cleanup of Virtual Machine instance %v because of --skip-if=%v\n", vm.ObjectMeta.Name, skipIf)
			return nil
		}
	}

	watch, err := client.VirtualMachineInstance(jctx.Namespace).Watch(ctx, *Selector(jctx))
	if err != nil {
		return err
	}
	defer watch.Stop()

	fmt.Fprintf(os.Stderr, "Deleting Virtual Machine instance %v\n", vm.ObjectMeta.Name)

	if err := client.VirtualMachineInstance(jctx.Namespace).Delete(ctx, vm.ObjectMeta.Name, nil); err != nil {
		return err
	}

	// Wait for VM to go away

	timeout, stop := context.WithTimeout(ctx, cmd.Timeout)
	defer stop()

	ch := watch.ResultChan()
	for {
		select {
		case event := <-ch:
			if event.Type == "DELETED" {
				return nil
			}
		case <-timeout.Done():
			return timeout.Err()
		}
	}
}
