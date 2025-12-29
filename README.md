# gitlab-autoscaler

#### how to build

```shell
# example build for apple macOS & Apple Silicon Chip
make build  OS=darwin ARCH=arm64
```

```shell
# build with Makefile in docker for apple macOS & Apple Silicon Chip
make docker-build OS=darwin ARCH=arm64
```

####  systemd service example

```unit file (systemd)
[Unit]
Description=GitLab Autoscaler
After=network.target

[Service]
Type=simple
User=gitlab-autoscaler
Group=gitlab-autoscaler
ExecStart=/usr/local/bin/gitlab-autoscaler -config /etc/gitlab-autoscaler/config.yml -pid-file /var/run/gitlab-autoscaler.pid
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure
PIDFile=/var/run/gitlab-autoscaler.pid

[Install]
WantedBy=multi-user.target


```
####  ./config.yml example
```yaml
autoscaler:                                    # Self autoscaler config
  check-interval: 10                           # This is a checks interval in seconds. Default is 10
aws:
  asg-names:                                   # An ASGs definition
    - name: 'my-gitlab-runner-amd64'           # ASG should exist with that name in region AWS_REGION
      scale-to-zero: true                      # Allow scale ASG to zero value. Default is false
      max-asg-capacity: 3                      # Maximum ASG capacity for that ASG. Default is 1  
      region: 'us-east-1'                      # AWS Region fot ASG. Default comes from AWS_REGION variable or in case of AWS_REGION does not exist from AWS_DEFAULT_REGION
      tags:                                    # Tags list to serve, also ASG trying to serve any job without tags if capacity allowed
        - amd64                                # GitLab job with tag amd64 will be served by this ASG
    - name: 'my-gitlab-runner-arm64'           # ASG should exist with that name in region AWS_REGION
      scale-to-zero: false                     # Do not allow scale ASG to zero value. Default is false
      max-asg-capacity: 4                      # Maximum ASG capacity for that ASG
      region: 'us-east-1'                      # AWS Region fot ASG. Default comes from AWS_REGION variable or in case of AWS_REGION does not exist from AWS_DEFAULT_REGION
      tags:                                    # Tags list to serve, also ASG trying to serve any job without tags if capacity allowed
        - arm64                                # GitLab job with tag arm64 will be served by this ASG
gitlab:                                        # GitLab settings
  token: 'private-gitlab-token'                # Private token with access to API
  group: 'mygroup'                             # Group name, all nested projects will be fetched and served
  exclude-projects:                            # except listed in exclude-projects:
    - 'project-without-ci'                     # Node Deployment will not be served  by Autoscaler; that means jobs will not be fetched.
```

#### Adding New Providers

To add support for a new cloud provider (e.g., Azure, GCP):

1. Create a new package under `./providers/<provider>` directory
2. Implement the `Provider` interface from `core/provider.go`:
   ```go
   type Provider interface {
       GetCurrentCapacity(asgName string) (int64, int64, error)
       UpdateASGCapacity(asgName string, capacity int64) error
   }
3. Add a provider-specific implementation in the new package (see ./providers/aws as an example)
4. Modify main.go to handle your new provider type: 
    ```go
    switch strings.ToLower(providerName) {
    case "aws":
        // existing AWS implementation
    case "<new-provider>": 
        client, err := <NewProvider>.NewClient(defaultRegion)
        // ...
    default:
        log.Fatalf("Unsupported provider '%s'", providerName)
    }
    ```
5. Add documentation about your new provider to the README

#### Contributing
We welcome contributions from the community! Here's how you can contribute:

1. Fork the repository and create a new branch for your feature/fix
2. Make sure all tests pass before submitting:
    ```bash
    go test ./...
    ```

3. Write clear commit messages following conventional commits style
4. Include documentation updates where necessary (especially in README.md)
5. Submit a pull request with a detailed description of your changes

Please ensure that:

- Your code follows the existing code style
- You've added appropriate tests for new functionality
- Your contribution addresses a specific problem or adds a useful feature
- You've updated documentation to reflect changes
Thanks for contributing to gitlab-autoscaler!