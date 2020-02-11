# gitlab2gitea [![Go Report Card](https://goreportcard.com/badge/cornelk/gitlab2gitea)](https://goreportcard.com/report/github.com/cornelk/gitlab2gitea)

A command line tool build with Golang to migrate a [GitLab](https://gitlab.com/) project to [Gitea](https://gitea.io/).

It uses the exposed API of both systems to migrate following data of a project:

* All open milestones
* All labels
* All open issues

It skips creation if an item already exists.

## Installation

You need to have Golang installed, otherwise follow the guide at [https://golang.org/doc/install](https://golang.org/doc/install).

```
go get github.com/cornelk/gitlab2gitea
```

## Usage

```
./gitlab2gitea --gitlabserver https://gitlab.domain.tld/ --gitlabtoken 12345 \
--giteaserver https://gitea.domain.tld/ --giteatoken 54321 \
--gitlabproject group/project --giteaproject group/project
```

## Options

```
Migrate labels, issues and milestones from GitLab to Gitea

Usage:
  gitlab2gitea [flags]

Flags:
      --config string          config file (default is $HOME/.gitlab2gitea.yaml)
      --giteaproject string    Gitea project name, use namespace/name. defaults to GitLab project name
      --giteaserver string     Gitea server URL
      --giteatoken string      token for Gitea API access
      --gitlabproject string   GitLab project name, use namespace/name
      --gitlabserver string    GitLab server URL with a trailing slash (default "https://gitlab.com/")
      --gitlabtoken string     token for GitLab API access
  -h, --help                   help for gitlab2gitea
```

## Golang SDK Documentations

[GitLab](https://pkg.go.dev/github.com/xanzy/go-gitlab)

[Gitea](https://pkg.go.dev/code.gitea.io/sdk/gitea)

## Dependencies

- [code.gitea.io/sdk/gitea](https://code.gitea.io/sdk/gitea) Gitea client
- [github.com/spf13/cobra](https://github.com/spf13/cobra) command line handling
- [github.com/spf13/viper](https://github.com/spf13/viper) configuration
- [github.com/xanzy/go-gitlab](https://github.com/xanzy/go-gitlab) GitLab client
- [go.uber.org/zap](https://go.uber.org/zap) logging
