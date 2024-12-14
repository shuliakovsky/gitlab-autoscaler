# gitlab-autoscaler

#### how to build

```shell
# example build for apple macOS & Apple Silicon Chip
./build.sh darwin arm64
```
```shell
# example build for apple macOS & Intel Chip
./build.sh darwin amd64
```
```shell
# build with no args for help
./build.sh 
```
```shell
# build with Makefile in docker for apple macOS & Apple Silicon Chip
make docker-build OS=darwin ARCH=arm64
```

####  systemd service example

```unit file (systemd)
[Unit]
  Description=Gitlab autoscaler
  Wants=network-online.target
  After=network-online.target

[Service]

  ExecStart=/usr/local/bin/gitlab-autoscaler --config /etc/gitlab-autoscaler/config.yml
  Environment=AWS_REGION=us-east-1
  SyslogIdentifier=gitlab-autoscaler
  Restart=always

[Install]
  WantedBy=multi-user.target

```
####  ./config.yml example
```yaml
autoscaler:                                    # Self autoscaler config
  check-interval: 10                           # This is a checks interval in seconds,
aws:
  scale-to-zero: true                          # Allow scale ASG to zero value
  asg-names:                                   # An ASGs definition
    - name: 'my-gitlab-runner-amd64'           # ASG should exist with that name in region AWS_REGION
      max-asg-capacity: 3                      # Maximum ASG capacity for that ASG
      tags:                                    # Tags list to serve, also ASG trying to serve any job without tags if capacity allowed
        - amd64                                # GitLab job with tag amd64 will be served by this ASG
    - name: 'my-gitlab-runner-arm64'           # ASG should exist with that name in region AWS_REGION
      max-asg-capacity: 4                      # Maximum ASG capacity for that ASG
      tags:                                    # Tags list to serve, also ASG trying to serve any job without tags if capacity allowed
        - arm64                                # GitLab job with tag arm64 will be served by this ASG

gitlab:                                        # GitLab settings
  token: 'private-gitlab-token'                # Private token with access to API
  group: 'mygroup'                             # Group name, all nested projects will be fetched and served
  exclude-projects:                            # except listed in exclude-projects:
    - 'project-without-ci'                     # Node Deployment will not be served  by Autoscaler; that means jobs will not be fetched.
```
