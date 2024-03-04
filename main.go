package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	openai "github.com/sashabaranov/go-openai"
	"github.com/urfave/cli/v2"
)

// func oaiCompletion(cCtx *cli.Context) {
// 	oai := oaiClient(cCtx)
// }

func noOmitFloat(f float32) float32 {
	if f == 0.0 {
		return math.SmallestNonzeroFloat32
	}
	return f
}

func oaiClient(cCtx *cli.Context) *openai.Client {
	endpoint := cCtx.String("ai_endpoint")
	config := openai.DefaultConfig("")
	config.BaseURL = endpoint
	return openai.NewClientWithConfig(config)
}

func query(cCtx *cli.Context, systemMessage string, userMessage string) string {
	SEED := 420
	oai := oaiClient(cCtx)
	resp, err := oai.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Temperature: noOmitFloat(0.0),
			TopP:        noOmitFloat(0.95),
			MaxTokens:   120,
			Seed:        &SEED,
			Stop:        []string{"</s>"},
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: systemMessage,
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: userMessage,
				},
			},
		},
	)

	if err != nil {
		fmt.Printf("ChatCompletion error: %v\n", err)
	}

	return strings.TrimSpace(resp.Choices[0].Message.Content)
}

type FileSummary struct {
	Name    string
	AbsPath string
	Summary string
	IsDir   bool
}

func matchesGlob(fileName string, globs []string) bool {
	for _, glob := range globs {
		match, err := filepath.Match(glob, fileName)
		if err != nil {
			fmt.Println("Error matching glob:", err)
			return false
		}
		if match {
			return true
		}
	}
	return false
}

func readSnip(filepath string) string {
	file, err := os.Open(filepath)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	buffer := make([]byte, 4000)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		log.Fatal(err)
	}

	return string(buffer[:n])
}

func sendFile(cCtx *cli.Context, file os.DirEntry, wg *sync.WaitGroup, results chan<- FileSummary) {
	defer wg.Done()
	directory := cCtx.String("directory")

	name := file.Name()
	path := filepath.Join(directory, name)
	absPath, _ := filepath.Abs(path)

	if file.IsDir() {
		results <- FileSummary{Name: name, IsDir: true, AbsPath: absPath, Summary: fmt.Sprintf("directory[%s]", absPath)}
	} else {
		aiResponse := query(cCtx, fmt.Sprintf("You are a code file summarizer assistant. Given the user's input, respond with a one sentence summary of what the file contains. The summary should be something that would be useful to see from a README file. The file's name is '%s'.", name), readSnip(absPath))
		results <- FileSummary{Name: name, IsDir: false, AbsPath: absPath, Summary: aiResponse}
	}
}

func readGitignorePatterns(gitignorePath string) ([]string, error) {
	file, err := os.Open(gitignorePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var patterns []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		trimmedLine := strings.TrimSpace(line)
		if trimmedLine == "" || strings.HasPrefix(trimmedLine, "#") {
			continue
		}
		patterns = append(patterns, trimmedLine)
	}

	return patterns, scanner.Err()
}

func main() {
	app := &cli.App{
		Name:  "ulexite",
		Usage: "automatically create descriptions of directories and the files within",

		EnableBashCompletion: true,
		Commands: []*cli.Command{
			{
				Name:    "query",
				Aliases: []string{"q"},
				Usage:   "query the specified openai-compatible endpoint",
				Action: func(cCtx *cli.Context) error {
					userQuery := cCtx.Args().First()
					if userQuery == "-" || userQuery == "" {
						stdin, err := io.ReadAll(os.Stdin)
						if err != nil {
							panic(err)
						}
						userQuery = string(stdin)
					}

					response := query(cCtx, "You are a file summarizer assistant. Given the user submitted file, provide a one sentence summary of what the file contains and what its purpose is.", userQuery)
					fmt.Println(response)

					return nil
				},
			},
			{
				Name:    "list",
				Aliases: []string{"ls"},
				Usage:   "list the given directory",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "directory",
						Aliases: []string{"d"},
						Usage:   "the directory to list",
						Value:   ".",
					},
				},
				Action: func(cCtx *cli.Context) error {
					globPatterns := []string{
						".*",
						"*.sum",
						"*.mod",
					}
					patterns, _ := readGitignorePatterns(".gitignore")
					globPatterns = append(globPatterns, patterns...)
					directory := cCtx.String("directory")
					files, _ := os.ReadDir(directory)

					var wg sync.WaitGroup
					results := make(chan FileSummary, len(files))
					for _, file := range files {
						if !matchesGlob(file.Name(), globPatterns) {
							wg.Add(1)
							go sendFile(cCtx, file, &wg, results)
						}
					}

					go func() {
						wg.Wait()
						close(results)
					}()

					var fileSummaries []FileSummary
					for result := range results {
						fileSummaries = append(fileSummaries, result)
					}

					sort.Slice(fileSummaries, func(i, j int) bool {
						return fileSummaries[i].Name < fileSummaries[j].Name
					})

					for _, summary := range fileSummaries {
						fmt.Printf("## %s\n\n%s\n\n", summary.Name, summary.Summary)
					}

					return nil
				},
			},
		},
		Flags: []cli.Flag{
			// &cli.BoolFlag{
			// 	Name:    "ai",
			// 	Aliases: []string{"a"},
			// 	Usage:   "use AI (via an openai-compatible endpoint) to automatically summarize files",
			// 	EnvVars: []string{"ULEXITE_AI"},
			// },
			&cli.StringFlag{
				Name:    "ai_endpoint",
				Value:   "http://localhost:8080/v1",
				Usage:   "the host and port for an openai-compatible endpoint to use for summarization",
				EnvVars: []string{"ULEXITE_AI_ENDPOINT"},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
