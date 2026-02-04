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

	"k8s.io/apimachinery/pkg/watch"
	kubevirtapi "kubevirt.io/api/core/v1"
	kubevirt "kubevirt.io/client-go/kubecli"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type CleanupCmd struct {
	Timeout time.Duration `name:"timeout" default:"1h"`
	SkipIf  []string      `name:"skip-if" sep:","`
}

func (cmd *CleanupCmd) Run(ctx context.Context, client kubevirt.KubevirtClient, jctx *JobContext) error {
	vm, err := FindJobVM(ctx, client, jctx)
	if err != nil {
		if !strings.Contains(err.Error(), "Virtual Machine instance disappeared while the job was running!") {
			return fmt.Errorf("cleanup error: %w", err) // Return the error only if it's NOT the specific "VM disappeared" error
		}

		fmt.Fprintf(os.Stderr, "Skipping cleanup of Virtual Machine instance because none were found\n")
		return nil
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

	fmt.Fprintf(os.Stderr, "Deleting Virtual Machine instance %v\n", vm.ObjectMeta.Name)

	deleteOptions := &metav1.DeleteOptions{}

	if err := client.VirtualMachineInstance(jctx.Namespace).Delete(ctx, vm.ObjectMeta.Name, *deleteOptions); err != nil {
		return err
	}

	timeout, stop := context.WithTimeout(ctx, cmd.Timeout)
	defer stop()

	// Wait for VM to go away

	return WatchJobVM(timeout, client, jctx, vm, func(et watch.EventType, _ *kubevirtapi.VirtualMachineInstance) error {
		switch et {
		case watch.Error:
			// We can't just retry like we do in prepare, because the deleted
			// machine might have gone away in the meantime, so we'd just block
			// forever.
			fmt.Fprintf(os.Stderr, "Couldn't wait for Virtual Machine instance to go away, abandoning it\n")
			return ErrWatchDone
		case watch.Deleted:
			return ErrWatchDone
		}
		return nil
	})
}
