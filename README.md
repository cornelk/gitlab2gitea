# gitlab2gitea [![Go Report Card](https://goreportcard.com/badge/cornelk/gitlab2gitea)](https://goreportcard.com/report/github.com/cornelk/gitlab2gitea)

A command line tool build with Golang to migrate a [GitLab](https://gitlab.com/) project to [Gitea](https://gitea.io/).

The Project status is **Work in Progress**

## Installation

You need to have Golang installed, otherwise follow the guide at [https://golang.org/doc/install](https://golang.org/doc/install).

```
go get github.com/cornelk/gitlab2gitea
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
