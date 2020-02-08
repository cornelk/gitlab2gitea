package main

import (
	"fmt"
	"strings"
	"time"

	"code.gitea.io/sdk/gitea"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/xanzy/go-gitlab"
	"go.uber.org/zap"
)

type migrator struct {
	cmd    *cobra.Command
	logger *zap.Logger

	gitlab          *gitlab.Client
	gitlabProjectID int

	gitea          *gitea.Client
	giteaProjectID int64
	giteaRepo      string
	giteaOwner     string
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
	rootCmd.Flags().String("gitlabproject", "", "GitLab project name, use namespace/name")

	rootCmd.Flags().String("giteatoken", "", "token for Gitea API access")
	rootCmd.Flags().String("giteaserver", "", "Gitea server URL")
	rootCmd.Flags().String("giteaproject", "", "Gitea project name, use namespace/name. defaults to GitLab project name")

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
	if err := m.migrateProject(); err != nil {
		m.logger.Fatal("Migrating the project failed", zap.Error(err))
	}
	m.logger.Info("Migration finished successfully")
}

func newMigrator(cmd *cobra.Command) *migrator {
	logger := logger(cmd)
	m := &migrator{
		cmd:    cmd,
		logger: logger,
	}

	m.gitlab = m.gitlabClient()
	m.gitea = m.giteaClient()
	return m
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

func (m *migrator) missingParameter(msg string) {
	_ = m.cmd.Help()
	m.logger.Fatal(msg)
}

// gitlabClient returns a new Gitlab client with the given command line
// parameters.
func (m *migrator) gitlabClient() *gitlab.Client {
	gitlabToken, _ := m.cmd.Flags().GetString("gitlabtoken")
	if gitlabToken == "" {
		m.missingParameter("No GitLab token given")
	}

	client := gitlab.NewClient(nil, gitlabToken)
	gitlabServer, _ := m.cmd.Flags().GetString("gitlabserver")
	if gitlabServer != "" {
		if err := client.SetBaseURL(gitlabServer); err != nil {
			m.logger.Fatal("Setting GitLab server URL failed", zap.Error(err))
		}
	}

	// get the user status to check that the auth and connection works
	_, _, err := client.Users.CurrentUserStatus()
	if err != nil {
		m.logger.Fatal("Getting GitLab user status failed", zap.Error(err))
	}

	gitlabProject, _ := m.cmd.Flags().GetString("gitlabproject")
	if gitlabProject == "" {
		m.missingParameter("No GitLab project given")
	}
	project, _, err := client.Projects.GetProject(gitlabProject, nil)
	if err != nil {
		m.logger.Fatal("Getting GitLab project info failed", zap.Error(err))
	}
	m.gitlabProjectID = project.ID

	return client
}

// giteaClient returns a new Gitea client with the given command line
// parameters.
func (m *migrator) giteaClient() *gitea.Client {
	giteaServer, _ := m.cmd.Flags().GetString("giteaserver")
	if giteaServer == "" {
		m.missingParameter("No Gitea server URL given")
	}

	giteaToken, _ := m.cmd.Flags().GetString("giteatoken")
	if giteaToken == "" {
		m.missingParameter("No Gitea token given")
	}

	client := gitea.NewClient(giteaServer, giteaToken)

	// get the user info to check that the auth and connection works
	_, err := client.GetMyUserInfo()
	if err != nil {
		m.logger.Fatal("Getting Gitea user info failed", zap.Error(err))
	}

	giteaProject, _ := m.cmd.Flags().GetString("giteaproject")
	if giteaProject == "" {
		giteaProject, _ = m.cmd.Flags().GetString("gitlabproject")
	}
	sl := strings.Split(giteaProject, "/")
	if len(sl) != 2 {
		m.missingParameter("Gitea project name uses wrong format")
	}
	m.giteaOwner = sl[0]
	m.giteaRepo = sl[1]

	repo, err := client.GetRepo(m.giteaOwner, m.giteaRepo)
	if err != nil {
		m.logger.Fatal("Getting Gitea repo info failed", zap.Error(err))
	}
	m.giteaProjectID = repo.ID

	return client
}

func (m *migrator) migrateProject() error {
	if err := m.migrateMilestones(); err != nil {
		return err
	}
	if err := m.migrateLables(); err != nil {
		return err
	}
	if err := m.migrateIssues(); err != nil {
		return err
	}
	return nil
}

func (m *migrator) migrateMilestones() error {
	giteaMilestones, err := m.gitea.ListRepoMilestones(m.giteaOwner, m.giteaRepo)
	if err != nil {
		return err
	}
	existing := map[string]struct{}{}
	for _, milestone := range giteaMilestones {
		existing[milestone.Title] = struct{}{}
	}

	state := "active"
	gitlabMilestones, _, err := m.gitlab.Milestones.ListMilestones(m.gitlabProjectID,
		&gitlab.ListMilestonesOptions{State: &state}, nil)
	if err != nil {
		return err
	}
	for _, milestone := range gitlabMilestones {
		if _, ok := existing[milestone.Title]; ok {
			continue
		}

		o := gitea.CreateMilestoneOption{
			Title:       milestone.Title,
			Description: milestone.Description,
			Deadline:    (*time.Time)(milestone.DueDate),
		}
		if _, err = m.gitea.CreateMilestone(m.giteaOwner, m.giteaRepo, o); err != nil {
			return err
		}
		m.logger.Info("Created milestone",
			zap.String("title", o.Title),
			zap.Time("deadline", *o.Deadline),
		)
	}
	return nil
}

func (m *migrator) migrateLables() error {
	giteaLabels, err := m.gitea.ListRepoLabels(m.giteaOwner, m.giteaRepo)
	if err != nil {
		return err
	}
	existing := map[string]struct{}{}
	for _, lable := range giteaLabels {
		existing[lable.Name] = struct{}{}
	}

	gitlabLables, _, err := m.gitlab.Labels.ListLabels(m.gitlabProjectID, nil, nil)
	if err != nil {
		return err
	}
	for _, lable := range gitlabLables {
		if _, ok := existing[lable.Name]; ok {
			continue
		}

		o := gitea.CreateLabelOption{
			Name:        lable.Name,
			Description: lable.Description,
			Color:       lable.Color,
		}
		if _, err = m.gitea.CreateLabel(m.giteaOwner, m.giteaRepo, o); err != nil {
			return err
		}
		m.logger.Info("Created label",
			zap.String("name", o.Name),
			zap.String("color", o.Color),
		)
	}
	return nil
}

func (m *migrator) migrateIssues() error {
	opt := gitea.ListIssueOption{
		State: "open",
	}
	giteaIssues, err := m.gitea.ListRepoIssues(m.giteaOwner, m.giteaRepo, opt)
	if err != nil {
		return err
	}
	existing := map[string]struct{}{}
	for _, issue := range giteaIssues {
		existing[issue.Title] = struct{}{}
	}

	giteaMilestones, err := m.giteaMilestones()
	if err != nil {
		return err
	}
	giteaLabels, err := m.giteaLabels()
	if err != nil {
		return err
	}

	gitlabIssues, _, err := m.gitlab.Issues.ListProjectIssues(m.gitlabProjectID, nil, nil)
	if err != nil {
		return err
	}
	for _, issue := range gitlabIssues {
		if _, ok := existing[issue.Title]; ok {
			continue
		}

		o := gitea.CreateIssueOption{
			Title:    issue.Title,
			Body:     issue.Description,
			Deadline: (*time.Time)(issue.DueDate),
		}

		milestone, ok := giteaMilestones[issue.Milestone.Title]
		if ok {
			o.Milestone = milestone.ID
		}

		for _, l := range issue.Labels {
			label, ok := giteaLabels[l]
			if ok {
				o.Labels = append(o.Labels, label.ID)
			}
		}

		if _, err = m.gitea.CreateIssue(m.giteaOwner, m.giteaRepo, o); err != nil {
			return err
		}
		m.logger.Info("Created issue",
			zap.String("title", o.Title),
		)
	}
	return nil
}

func (m *migrator) giteaMilestones() (map[string]*gitea.Milestone, error) {
	giteaMilestones, err := m.gitea.ListRepoMilestones(m.giteaOwner, m.giteaRepo)
	if err != nil {
		return nil, err
	}
	milestones := map[string]*gitea.Milestone{}
	for _, milestone := range giteaMilestones {
		milestones[milestone.Title] = milestone
	}
	return milestones, nil
}

func (m *migrator) giteaLabels() (map[string]*gitea.Label, error) {
	giteaLabels, err := m.gitea.ListRepoLabels(m.giteaOwner, m.giteaRepo)
	if err != nil {
		return nil, err
	}
	labels := map[string]*gitea.Label{}
	for _, label := range giteaLabels {
		labels[label.Name] = label
	}
	return labels, nil
}
