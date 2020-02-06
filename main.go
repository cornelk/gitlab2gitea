package main

import (
	"fmt"

	"code.gitea.io/sdk/gitea"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/xanzy/go-gitlab"
	"go.uber.org/zap"
)

type migrator struct {
	cmd    *cobra.Command
	logger *zap.Logger
	gitl   *gitlab.Client
	giti   *gitea.Client
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "gitlab2gitea",
		Short: "Migrate labels, issues and milestones from GitLab to Gitea",
		Run:   startMigration,
	}

	rootCmd.Flags().String("config", "", "config file (default is $HOME/.gitlab2gitea.yaml)")
	rootCmd.Flags().String("gitlabtoken", "", "token for GitLab API access")
	rootCmd.Flags().String("gitlabserver", "https://gitlab.com/", "GitLab server URL with a trailing slash")
	rootCmd.Flags().String("giteatoken", "", "token for Gitea API access")
	rootCmd.Flags().String("giteaserver", "", "Gitea server URL")

	if err := rootCmd.Execute(); err != nil {
		fmt.Printf("ERROR: %v\n", err)
	}
}

func startMigration(cmd *cobra.Command, args []string) {
	configFile, err := cmd.Flags().GetString("config")
	if err == nil && configFile != "" { // enable ability to specify config file via flag
		viper.SetConfigFile(configFile)
	}

	viper.SetConfigName(".gitlab2gitea") // name of config file (without extension)
	viper.AddConfigPath("$HOME")         // adding home directory as first search path
	viper.AutomaticEnv()                 // read in environment variables that match

	_ = viper.ReadInConfig()

	m := newMigrator(cmd)
	m.migrateProject()
}

func logger(cmd *cobra.Command) *zap.Logger {
	config := zap.NewDevelopmentConfig()
	config.Development = false
	config.DisableCaller = true
	config.DisableStacktrace = true

	level := config.Level
	verbose, _ := cmd.Flags().GetBool("verbose")
	if verbose {
		level.SetLevel(zap.DebugLevel)
	} else {
		level.SetLevel(zap.InfoLevel)
	}

	log, _ := config.Build()
	return log
}

func newMigrator(cmd *cobra.Command) *migrator {
	logger := logger(cmd)
	m := &migrator{
		cmd:    cmd,
		logger: logger,
	}

	m.gitl = m.gitlabClient()
	m.giti = m.giteaClient()
	return m
}

// gitlabClient returns a new Gitlab client with the given command line
// parameters.
func (m *migrator) gitlabClient() *gitlab.Client {
	gitlabToken, _ := m.cmd.Flags().GetString("gitlabtoken")
	if gitlabToken == "" {
		m.logger.Fatal("No GitLab token given")
	}

	gitl := gitlab.NewClient(nil, gitlabToken)
	gitlabServer, _ := m.cmd.Flags().GetString("gitlabserver")
	if gitlabServer != "" {
		if err := gitl.SetBaseURL(gitlabServer); err != nil {
			m.logger.Fatal("Setting GitLab server URL failed", zap.Error(err))
		}
	}

	// get the user status to check that the auth and connection works
	_, _, err := gitl.Users.CurrentUserStatus()
	if err != nil {
		m.logger.Fatal("Getting GitLab user status failed", zap.Error(err))
	}
	return gitl
}

// giteaClient returns a new Gitea client with the given command line
// parameters.
func (m *migrator) giteaClient() *gitea.Client {
	giteaServer, _ := m.cmd.Flags().GetString("giteaserver")
	if giteaServer == "" {
		m.logger.Fatal("No Gitea server URL given")
	}

	giteaToken, _ := m.cmd.Flags().GetString("giteatoken")
	if giteaToken == "" {
		m.logger.Fatal("No Gitea token given")
	}

	giti := gitea.NewClient(giteaServer, giteaToken)

	// get the user info to check that the auth and connection works
	_, err := giti.GetMyUserInfo()
	if err != nil {
		m.logger.Fatal("Getting Gitea user info failed", zap.Error(err))
	}
	return giti
}

func (m *migrator) migrateProject() {
	// TODO implement
}
