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

// startMigration is the entry point for the command.
func startMigration(cmd *cobra.Command, _ []string) {
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

// newMigrator returns a new creator object.
// It also tests that Gitlab and gitea can be reached.
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

// logger returns a new logger instance.
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

	gitlabServer, _ := m.cmd.Flags().GetString("gitlabserver")
	client, err := gitlab.NewClient(gitlabToken, gitlab.WithBaseURL(gitlabServer))
	if err != nil {
		m.logger.Fatal("Creating Gitlab client failed", zap.Error(err))
	}

	// get the user status to check that the auth and connection works
	_, _, err = client.Users.CurrentUserStatus()
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

	client, err := gitea.NewClient(giteaServer, gitea.SetToken(giteaToken))
	if err != nil {
		m.logger.Fatal("Creating Gitea failed", zap.Error(err))
	}

	// get the user info to check that the auth and connection works
	_, _, err = client.GetMyUserInfo()
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

	repo, _, err := client.GetRepo(m.giteaOwner, m.giteaRepo)
	if err != nil {
		m.logger.Fatal("Getting Gitea repo info failed", zap.Error(err))
	}
	m.giteaProjectID = repo.ID

	return client
}

// migrateProject migrates all supported aspects of a project.
func (m *migrator) migrateProject() error {
	m.logger.Info("Migrating milestones")
	if err := m.migrateMilestones(); err != nil {
		return fmt.Errorf("migrating milestones: %w", err)
	}

	m.logger.Info("Migrating labels")
	if err := m.migrateLabels(); err != nil {
		return fmt.Errorf("migrating labels: %w", err)
	}

	m.logger.Info("Migrating issues")
	if err := m.migrateIssues(); err != nil {
		return fmt.Errorf("migrating issues: %w", err)
	}
	return nil
}

// migrateMilestones does the active milestones migration.
func (m *migrator) migrateMilestones() error {
	existing, err := m.giteaMilestones()
	if err != nil {
		return err
	}

	state := "active"
	for page := 1; ; page++ {
		opt := &gitlab.ListMilestonesOptions{
			ListOptions: gitlab.ListOptions{
				Page:    page,
				PerPage: 100,
			},
			State: &state,
		}

		gitlabMilestones, _, err := m.gitlab.Milestones.ListMilestones(m.gitlabProjectID, opt, nil)
		if err != nil {
			return err
		}
		if len(gitlabMilestones) == 0 {
			return nil
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
			if _, _, err = m.gitea.CreateMilestone(m.giteaOwner, m.giteaRepo, o); err != nil {
				return err
			}
			m.logger.Info("Created milestone", zap.String("title", o.Title))
		}
	}
}

// migrateLabels migrates all labels.
func (m *migrator) migrateLabels() error {
	existing, err := m.giteaLabels()
	if err != nil {
		return err
	}

	for page := 1; ; page++ {
		opt := &gitlab.ListLabelsOptions{
			ListOptions: gitlab.ListOptions{
				Page:    page,
				PerPage: 100,
			},
		}

		gitlabLabels, _, err := m.gitlab.Labels.ListLabels(m.gitlabProjectID, opt, nil)
		if err != nil {
			return err
		}
		if len(gitlabLabels) == 0 {
			return nil
		}

		for _, label := range gitlabLabels {
			if _, ok := existing[label.Name]; ok {
				continue
			}

			o := gitea.CreateLabelOption{
				Name:        label.Name,
				Description: label.Description,
				Color:       label.Color,
			}
			if _, _, err = m.gitea.CreateLabel(m.giteaOwner, m.giteaRepo, o); err != nil {
				return err
			}
			m.logger.Info("Created label",
				zap.String("name", o.Name),
				zap.String("color", o.Color),
			)
		}
	}
}

// migrateIssues migrates all open issues.
func (m *migrator) migrateIssues() error {
	giteaIssues, err := m.giteaIssues()
	if err != nil {
		return err
	}
	giteaMilestones, err := m.giteaMilestones()
	if err != nil {
		return err
	}
	giteaLabels, err := m.giteaLabels()
	if err != nil {
		return err
	}

	state := "opened"
	for page := 1; ; page++ {
		opt := &gitlab.ListProjectIssuesOptions{
			ListOptions: gitlab.ListOptions{
				Page:    page,
				PerPage: 100,
			},
			State: &state,
		}

		gitlabIssues, _, err := m.gitlab.Issues.ListProjectIssues(m.gitlabProjectID, opt, nil)
		if err != nil {
			return err
		}
		if len(gitlabIssues) == 0 {
			return nil
		}

		for _, issue := range gitlabIssues {
			if err = m.migrateIssue(issue, giteaMilestones, giteaLabels, giteaIssues); err != nil {
				return err
			}
		}
	}
}

// migrateIssue migrates a single issue.
func (m *migrator) migrateIssue(issue *gitlab.Issue, giteaMilestones map[string]*gitea.Milestone,
	giteaLabels map[string]*gitea.Label, giteaIssues map[string]*gitea.Issue) error {
	o := gitea.CreateIssueOption{
		Title:    issue.Title,
		Body:     issue.Description,
		Deadline: (*time.Time)(issue.DueDate),
	}

	if issue.Milestone != nil {
		milestone, ok := giteaMilestones[issue.Milestone.Title]
		if ok {
			o.Milestone = milestone.ID
		} else {
			m.logger.Error("Unknown milestone",
				zap.String("milestone", issue.Milestone.Title),
			)
		}
	}

	for _, l := range issue.Labels {
		label, ok := giteaLabels[l]
		if ok {
			o.Labels = append(o.Labels, label.ID)
		} else {
			m.logger.Error("Unknown label",
				zap.String("label", l),
			)
		}
	}

	existing, ok := giteaIssues[issue.Title]
	if !ok {
		if _, _, err := m.gitea.CreateIssue(m.giteaOwner, m.giteaRepo, o); err != nil {
			return err
		}
		m.logger.Info("Created issue",
			zap.String("title", o.Title),
		)
		return nil
	}

	editOptions := gitea.EditIssueOption{
		Title:     o.Title,
		Body:      &o.Body,
		Milestone: &o.Milestone,
		Deadline:  o.Deadline,
	}
	if _, _, err := m.gitea.EditIssue(m.giteaOwner, m.giteaRepo, existing.Index, editOptions); err != nil {
		return err
	}
	labelOptions := gitea.IssueLabelsOption{
		Labels: o.Labels,
	}
	if _, _, err := m.gitea.ReplaceIssueLabels(m.giteaOwner, m.giteaRepo, existing.Index, labelOptions); err != nil {
		return err
	}
	m.logger.Info("Updated issue",
		zap.String("title", o.Title),
	)
	return nil
}

// giteaMilestones returns a map of all gitea milestones.
func (m *migrator) giteaMilestones() (map[string]*gitea.Milestone, error) {
	opt := gitea.ListMilestoneOption{
		State: "all",
	}
	giteaMilestones, _, err := m.gitea.ListRepoMilestones(m.giteaOwner, m.giteaRepo, opt)
	if err != nil {
		return nil, err
	}
	milestones := map[string]*gitea.Milestone{}
	for _, milestone := range giteaMilestones {
		milestones[milestone.Title] = milestone
	}
	return milestones, nil
}

// giteaMilestones returns a map of all gitea labels.
func (m *migrator) giteaLabels() (map[string]*gitea.Label, error) {
	opt := gitea.ListLabelsOptions{}
	giteaLabels, _, err := m.gitea.ListRepoLabels(m.giteaOwner, m.giteaRepo, opt)
	if err != nil {
		return nil, err
	}
	labels := map[string]*gitea.Label{}
	for _, label := range giteaLabels {
		labels[label.Name] = label
	}
	return labels, nil
}

// giteaMilestones returns a map of all gitea issues.
func (m *migrator) giteaIssues() (map[string]*gitea.Issue, error) {
	issues := map[string]*gitea.Issue{}
	for page := 1; ; page++ {
		opt := gitea.ListIssueOption{
			ListOptions: gitea.ListOptions{
				Page: page,
			},
			State: "all",
		}
		giteaIssues, _, err := m.gitea.ListRepoIssues(m.giteaOwner, m.giteaRepo, opt)
		if err != nil {
			return nil, err
		}
		if len(giteaIssues) == 0 {
			return issues, nil
		}

		for _, issue := range giteaIssues {
			issues[issue.Title] = issue
		}
	}
}
