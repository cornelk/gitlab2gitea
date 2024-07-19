package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"code.gitea.io/sdk/gitea"
	"github.com/alexflint/go-arg"
	"github.com/cornelk/gotokit/env"
	"github.com/cornelk/gotokit/log"
	"github.com/xanzy/go-gitlab"
)

type arguments struct {
	GitlabToken   string `arg:"--gitlabtoken,required" help:"token for GitLab API access"`
	GitlabServer  string `arg:"--gitlabserver" help:"GitLab server URL with a trailing slash"`
	GitlabProject string `arg:"--gitlabproject,required" help:"GitLab project name, use namespace/name"`
	GiteaToken    string `arg:"--giteatoken,required" help:"token for Gitea API access"`
	GiteaServer   string `arg:"--giteaserver,required" help:"Gitea server URL"`
	GiteaProject  string `arg:"--giteaproject" help:"Gitea project name, use namespace/name. defaults to GitLab project name"`
}

func (arguments) Description() string {
	return "Migrate labels, issues and milestones from GitLab to Gitea.\n"
}

type migrator struct {
	args   arguments
	logger *log.Logger

	gitlab          *gitlab.Client
	gitlabProjectID int

	gitea          *gitea.Client
	giteaProjectID int64
	giteaRepo      string
	giteaOwner     string
}

func main() {
	args, err := readArguments()
	if err != nil {
		fmt.Printf("Reading arguments failed: %s\n", err)
		os.Exit(1)
	}

	logger, err := createLogger()
	if err != nil {
		fmt.Printf("Creating logger failed: %s\n", err)
		os.Exit(1)
	}

	m, err := newMigrator(args, logger)
	if err != nil {
		logger.Fatal("Creating migrator failed", log.Err(err))
	}

	if err := m.migrateProject(); err != nil {
		m.logger.Fatal("Migrating the project failed", log.Err(err))
	}

	m.logger.Info("Migration finished successfully")
}

func readArguments() (arguments, error) {
	var args arguments
	parser, err := arg.NewParser(arg.Config{}, &args)
	if err != nil {
		return arguments{}, fmt.Errorf("creating argument parser: %w", err)
	}

	if err = parser.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, arg.ErrHelp) || errors.Is(err, arg.ErrVersion) {
			parser.WriteHelp(os.Stdout)
			os.Exit(0)
		}

		return arguments{}, fmt.Errorf("parsing arguments: %w", err)
	}

	return args, nil
}

func createLogger() (*log.Logger, error) {
	cfg, err := log.ConfigForEnv(env.Development)
	if err != nil {
		return nil, fmt.Errorf("initializing log config: %w", err)
	}
	cfg.JSONOutput = false
	cfg.CallerInfo = false

	logger, err := log.NewWithConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("initializing logger: %w", err)
	}
	return logger, nil
}

// newMigrator returns a new creator object.
// It also tests that Gitlab and gitea can be reached.
func newMigrator(args arguments, logger *log.Logger) (*migrator, error) {
	m := &migrator{
		args:   args,
		logger: logger,
	}

	var err error
	m.gitlab, err = m.gitlabClient()
	if err != nil {
		return nil, err
	}

	m.gitea, err = m.giteaClient()
	if err != nil {
		return nil, err
	}

	return m, nil
}

// gitlabClient returns a new Gitlab client with the given command line parameters.
func (m *migrator) gitlabClient() (*gitlab.Client, error) {
	client, err := gitlab.NewClient(m.args.GitlabToken, gitlab.WithBaseURL(m.args.GitlabServer))
	if err != nil {
		return nil, fmt.Errorf("creating Gitlab client: %w", err)
	}

	// get the user status to check that the auth and connection works
	_, _, err = client.Users.CurrentUserStatus()
	if err != nil {
		return nil, fmt.Errorf("getting GitLab user status: %w", err)
	}

	project, _, err := client.Projects.GetProject(m.args.GitlabProject, nil)
	if err != nil {
		return nil, fmt.Errorf("getting GitLab project info: %w", err)
	}
	m.gitlabProjectID = project.ID

	return client, nil
}

// giteaClient returns a new Gitea client with the given command line parameters.
func (m *migrator) giteaClient() (*gitea.Client, error) {
	client, err := gitea.NewClient(m.args.GiteaServer, gitea.SetToken(m.args.GiteaToken))
	if err != nil {
		return nil, fmt.Errorf("creating Gitea client: %w", err)
	}

	// get the user info to check that the auth and connection works
	_, _, err = client.GetMyUserInfo()
	if err != nil {
		return nil, fmt.Errorf("getting Gitea user info: %w", err)
	}

	giteaProject := m.args.GiteaProject
	if giteaProject == "" {
		giteaProject = m.args.GitlabProject
	}
	sl := strings.Split(giteaProject, "/")
	if len(sl) != 2 {
		return nil, errors.New("wrong format of Gitea project name")
	}
	m.giteaOwner = sl[0]
	m.giteaRepo = sl[1]

	repo, _, err := client.GetRepo(m.giteaOwner, m.giteaRepo)
	if err != nil {
		return nil, fmt.Errorf("getting Gitea repo info: %w", err)
	}
	m.giteaProjectID = repo.ID

	return client, nil
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
			m.logger.Info("Created milestone", log.String("title", o.Title))
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
				log.String("name", o.Name),
				log.String("color", o.Color),
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
			m.logger.Error("Unknown milestone", log.String("milestone", issue.Milestone.Title))
		}
	}

	for _, l := range issue.Labels {
		label, ok := giteaLabels[l]
		if ok {
			o.Labels = append(o.Labels, label.ID)
		} else {
			m.logger.Error("Unknown label", log.String("label", l))
		}
	}

	existing, ok := giteaIssues[issue.Title]
	if !ok {
		if _, _, err := m.gitea.CreateIssue(m.giteaOwner, m.giteaRepo, o); err != nil {
			return err
		}
		m.logger.Info("Created issue", log.String("title", o.Title))
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

	m.logger.Info("Updated issue", log.String("title", o.Title))
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
