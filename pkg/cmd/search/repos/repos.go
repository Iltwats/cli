package repos

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cli/cli/v2/internal/config"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/cli/cli/v2/pkg/search"
	"github.com/cli/cli/v2/pkg/text"
	"github.com/cli/cli/v2/utils"
	"github.com/spf13/cobra"
)

type ReposOptions struct {
	Browser    cmdutil.Browser
	Config     func() (config.Config, error)
	Exporter   cmdutil.Exporter
	HttpClient func() (*http.Client, error)
	IO         *iostreams.IOStreams
	Query      search.Query
	Searcher   search.Searcher
	WebMode    bool
}

func NewCmdRepos(f *cmdutil.Factory, runF func(*ReposOptions) error) *cobra.Command {
	opts := &ReposOptions{
		Browser:    f.Browser,
		Config:     f.Config,
		HttpClient: f.HttpClient,
		IO:         f.IOStreams,
		Query:      search.Query{Kind: search.KindRepositories},
	}

	cmd := &cobra.Command{
		Use:   "repos [<query>]",
		Short: "Search repositories",
		RunE: func(c *cobra.Command, args []string) error {
			opts.Query.Keywords = args
			if opts.Query.Limit < 1 || opts.Query.Limit > 1000 {
				return cmdutil.FlagErrorf("`--limit` must be between 1 and 1000")
			}
			if runF != nil {
				return runF(opts)
			}
			return reposRun(opts)
		},
	}

	// Output flags
	cmdutil.AddJSONFlags(cmd, &opts.Exporter, search.RepositoryFields)
	cmd.Flags().BoolVarP(&opts.WebMode, "web", "w", false, "Open the query in the web browser")

	// Query parameter flags
	cmd.Flags().IntVarP(&opts.Query.Limit, "limit", "L", 30, "Maximum number of repositories to fetch")
	cmdutil.StringEnumFlag(cmd, &opts.Query.Order, "order", "", "", []string{"asc", "desc"}, "Order of repositories returned, ignored unless '--sort' is specified")
	cmdutil.StringEnumFlag(cmd, &opts.Query.Sort, "sort", "", "", []string{"forks", "help-wanted-issues", "stars", "updated"}, "Sorts the repositories by stars, forks, help-wanted-issues, or updated")

	// Query qualifier flags
	cmdutil.NilBoolFlag(cmd, &opts.Query.Qualifiers.Archived, "archived", "", "Filter based on archive state")
	cmd.Flags().StringVar(&opts.Query.Qualifiers.Created, "created", "", "Filter based on created at date")
	cmd.Flags().StringVar(&opts.Query.Qualifiers.Followers, "followers", "", "Filter based on number of followers")
	cmdutil.StringEnumFlag(cmd, &opts.Query.Qualifiers.Fork, "include-forks", "", "", []string{"false", "true", "only"}, "Include forks in search")
	cmd.Flags().StringVar(&opts.Query.Qualifiers.Forks, "forks", "", "Filter on number of forks")
	cmd.Flags().StringVar(&opts.Query.Qualifiers.GoodFirstIssues, "good-first-issues", "", "Filter on number of issues with the 'good first issue' label")
	cmd.Flags().StringVar(&opts.Query.Qualifiers.HelpWantedIssues, "help-wanted-issues", "", "Filter on number of issues with the 'help wanted' label")
	cmdutil.StringSliceEnumFlag(cmd, &opts.Query.Qualifiers.In,
		"in", "", nil, []string{"name", "description", "readme"}, "Restrict search to the name, description, or README file")
	cmd.Flags().StringSliceVar(&opts.Query.Qualifiers.Language, "language", nil, "Filter based on the coding language")
	cmd.Flags().StringSliceVar(&opts.Query.Qualifiers.License, "license", nil, "Filter based on license type")
	cmdutil.NilBoolFlag(cmd, &opts.Query.Qualifiers.Mirror, "mirror", "", "Filter based on mirror state")
	cmd.Flags().StringVar(&opts.Query.Qualifiers.Org, "org", "", "Filter on organization")
	cmd.Flags().StringVar(&opts.Query.Qualifiers.Pushed, "updated", "", "Filter on last updated at date")
	cmd.Flags().StringVar(&opts.Query.Qualifiers.Repo, "repo", "", "Filter on repository name")
	cmd.Flags().StringVar(&opts.Query.Qualifiers.Size, "size", "", "Filter on a size range, in kilobytes")
	cmd.Flags().StringVar(&opts.Query.Qualifiers.Stars, "stars", "", "Filter on number of stars")
	cmd.Flags().StringSliceVar(&opts.Query.Qualifiers.Topic, "topic", nil, "Filter on topic")
	cmd.Flags().StringVar(&opts.Query.Qualifiers.Topics, "number-topics", "", "Filter on number of topics")
	cmd.Flags().StringVar(&opts.Query.Qualifiers.User, "user", "", "Filter based on user")
	cmdutil.StringEnumFlag(cmd, &opts.Query.Qualifiers.Is, "visibility", "", "", []string{"public", "private"}, "Filter based on visibility")

	return cmd
}

func reposRun(opts *ReposOptions) error {
	io := opts.IO
	if opts.Searcher == nil {
		cfg, err := opts.Config()
		if err != nil {
			return err
		}
		host, err := cfg.DefaultHost()
		if err != nil {
			return err
		}
		client, err := opts.HttpClient()
		if err != nil {
			return err
		}
		opts.Searcher = search.NewSearcher(client, host)
	}
	if opts.WebMode {
		url := opts.Searcher.URL(opts.Query)
		if io.IsStdoutTTY() {
			fmt.Fprintf(io.ErrOut, "Opening %s in your browser.\n", utils.DisplayURL(url))
		}
		return opts.Browser.Browse(url)
	}
	io.StartProgressIndicator()
	result, err := opts.Searcher.Repositories(opts.Query)
	io.StopProgressIndicator()
	if err != nil {
		return err
	}
	if err := io.StartPager(); err == nil {
		defer io.StopPager()
	} else {
		fmt.Fprintf(io.ErrOut, "failed to start pager: %v\n", err)
	}
	if opts.Exporter != nil {
		return opts.Exporter.Write(io, result.Items)
	}
	return displayResults(io, result)
}

func displayResults(io *iostreams.IOStreams, results search.RepositoriesResult) error {
	cs := io.ColorScheme()
	tp := utils.NewTablePrinter(io)
	for _, repo := range results.Items {
		var tags []string
		if repo.Private {
			tags = append(tags, "private")
		} else {
			tags = append(tags, "public")
		}
		if repo.Fork {
			tags = append(tags, "fork")
		}
		if repo.Archived {
			tags = append(tags, "archived")
		}
		info := strings.Join(tags, ", ")
		infoColor := cs.Gray
		if repo.Private {
			infoColor = cs.Yellow
		}
		tp.AddField(repo.FullName, nil, cs.Bold)
		description := repo.Description
		tp.AddField(text.ReplaceExcessiveWhitespace(description), nil, nil)
		tp.AddField(info, nil, infoColor)
		if tp.IsTTY() {
			tp.AddField(utils.FuzzyAgoAbbr(time.Now(), repo.UpdatedAt), nil, cs.Gray)
		} else {
			tp.AddField(repo.UpdatedAt.Format(time.RFC3339), nil, nil)
		}
		tp.EndRow()
	}
	if io.IsStdoutTTY() {
		header := "No repositories matched your search\n"
		if len(results.Items) > 0 {
			header = fmt.Sprintf("Showing %d of %d repositories\n\n", len(results.Items), results.Total)
		}
		fmt.Fprintf(io.Out, "\n%s", header)
	}
	return tp.Render()
}
