# gitlab-runner-kubevirt

A Gitlab Runner custom executor for running jobs in VMs on kubernetes

## Usage

Before you begin, make sure that KubeVirt has been installed
to the cluster where you want to run these VM jobs.

### Manual Configuration

Add this configuration to your runner:

```toml
[[runners]]
name = "kubevirt"
executor = "custom"

  [runners.custom]
  config_exec = "/bin/gitlab-runner-kubevirt"
  config_args = ["config"]
  prepare_exec = "/bin/gitlab-runner-kubevirt"
  prepare_args = [
    "prepare",
    "--shell", "bash",
    <prepare config flags ...>
  ]
  run_exec = "/bin/gitlab-runner-kubevirt"
  run_args = [
    "run",
    <run config flags ...>
  ]
  cleanup_exec = "/bin/gitlab-runner-kubevirt"
  cleanup_args = ["cleanup"]
```

Various aspects of the virtual machines can be

### Using the gitlab-runner helm chart

The gitlab-runner-kubevirt executor can be used with the official gitlab-runner
helm chart. Simply add the following to the values:

```yaml
image: ghcr.io/snaipe/gitlab-runner-kubevirt:main

runners:
  executor: custom
  config: |
    [[runners]]
    name = "kubevirt"
    executor = "custom"

      [runners.custom]
      config_exec = "/bin/gitlab-runner-kubevirt"
      config_args = ["config"]
      prepare_exec = "/bin/gitlab-runner-kubevirt"
      prepare_args = ["prepare", "--shell", "bash"]
      run_exec = "/bin/gitlab-runner-kubevirt"
      run_args = ["run"]
      cleanup_exec = "/bin/gitlab-runner-kubevirt"
      cleanup_args = ["cleanup"]
```

## Examples

### Setting up a Windows runner with 2 CPUs and 4GB memory

```toml
[[runners]]
name = "kubernetes-windows-kubevirt"
executor = "custom"
shell = "pwsh"

  [runners.custom]
  config_exec = "/bin/gitlab-runner-kubevirt"
  config_args = ["config"]
  prepare_exec = "/bin/gitlab-runner-kubevirt"
  prepare_args = [
    "prepare",
    "--debug",
    "--shell", "pwsh",
    "--default-image", "gitlab-registry.example.com:5050/ci/windows:v0.0.1"
    "--default-memory-request", "4G",
    "--default-memory-limit", "4G",
    "--default-cpu-request", "2",
    "--default-cpu-limit", "2",
    "--default-ephemeral-storage-request", "30Gi",
    "--default-ephemeral-storage-limit", "60Gi",
    "--default-image-pull-secret", "gitlab-registry-credentials",
    "--timeout", "1h",
  ]
  run_exec = "/bin/gitlab-runner-kubevirt"
  run_args = [
    "run",
    "--ssh-user", "vagrant",
    "--ssh-password", "vagrant",
  ]
  cleanup_exec = "/bin/gitlab-runner-kubevirt"
  cleanup_args = ["cleanup"]
```
