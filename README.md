# gitlab2gitea [![Go Report Card](https://goreportcard.com/badge/cornelk/gitlab2gitea)](https://goreportcard.com/report/github.com/cornelk/gitlab2gitea)

A command line tool build with Golang to migrate a [GitLab](https://gitlab.com/) project to [Gitea](https://gitea.io/).

The Project status is **Work in Progress**

## Installation

You need to have Golang installed, otherwise follow the guide at [https://golang.org/doc/install](https://golang.org/doc/install).

```
go get github.com/cornelk/gitlab2gitea
```

## Dependencies

- [github.com/spf13/cobra](https://github.com/spf13/cobra) command line handling
- [github.com/spf13/viper](https://github.com/spf13/viper) configuration
- [go.uber.org/zap](https://go.uber.org/zap) logging
