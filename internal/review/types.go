package review

import "github.com/awhg23/pr-go/internal/github"

type Input struct {
	Owner          string
	Repo           string
	Number         int
	Title          string
	Description    string
	AuthorLogin    string
	BaseSHA        string
	HeadSHA        string
	ChangedFiles   []FileDiff
	Diff           string
	DiffTruncated  bool
	OmittedBytes   int
	MaxDiffBytes   int
	ChangedCount   int
	TotalAdditions int
	TotalDeletions int
}

type FileDiff struct {
	Path      string
	Status    string
	Additions int
	Deletions int
	Changes   int
	Patch     string
	Truncated bool
}

type Result struct {
	Summary  string    `json:"summary"`
	Findings []Finding `json:"findings"`
	Risk     Risk      `json:"risk"`
}

type Finding struct {
	ID         string `json:"id"`
	FilePath   string `json:"file_path"`
	LineNumber int    `json:"line_number,omitempty"`
	Severity   string `json:"severity"`
	Category   string `json:"category"`
	Title      string `json:"title"`
	Reason     string `json:"reason"`
	Suggestion string `json:"suggestion"`
}

type Risk struct {
	Score   int      `json:"score"`
	Level   string   `json:"level"`
	Reasons []string `json:"reasons"`
}

func BuildInput(pr github.PullRequest, maxDiffBytes int) Input {
	files := make([]FileDiff, 0, len(pr.Files))
	input := Input{
		Owner:        pr.Ref.Owner,
		Repo:         pr.Ref.Repo,
		Number:       pr.Ref.Number,
		Title:        pr.Title,
		Description:  pr.Body,
		AuthorLogin:  pr.AuthorLogin,
		BaseSHA:      pr.BaseSHA,
		HeadSHA:      pr.HeadSHA,
		MaxDiffBytes: maxDiffBytes,
		ChangedCount: len(pr.Files),
	}

	for _, file := range pr.Files {
		input.TotalAdditions += file.Additions
		input.TotalDeletions += file.Deletions
		files = append(files, FileDiff{
			Path:      file.Filename,
			Status:    file.Status,
			Additions: file.Additions,
			Deletions: file.Deletions,
			Changes:   file.Changes,
			Patch:     file.Patch,
		})
	}

	input.ChangedFiles = files
	input.Diff, input.DiffTruncated, input.OmittedBytes, input.ChangedFiles = CompressDiff(files, maxDiffBytes)
	return input
}
