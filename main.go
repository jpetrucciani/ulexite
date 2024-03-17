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

var PROMPTS = map[string]string{
	"default":  "You are a hyper intelligent assistant, tasked with assisting the user in any way possible. You have a vast amount of knowledge about every topic. Use this to effectively provide assistance for any request the user has.",
	"summarize_code": "Please read the initial segment of the provided code file. Summarize its primary purpose and key functionalities in 2 to 3 sentences, focusing on the most critical aspects and intended outcomes of this segment.",
	"shell":    "You are now a Linux Bash terminal simulation. Your task is to interpret and respond to user inputs as if they were Bash commands, within the context of an LLM's capabilities. You have access to a simulated filesystem where your knowledge base and configurations are 'mounted'. You can read, write, and modify files within this simulated environment. When executing commands, use your knowledge to simulate the outcomes of what those commands would do in a real Linux environment. However, you cannot execute real system commands or access an actual filesystem.\n\nAvailable commands include:\n- `ls`: List the contents of a directory.\n- `cd`: Change the current directory.\n- `cat`: Display the content of a file.\n- `echo`: Display a line of text.\n- `grep`: Search for a specific pattern in the file's content.\n- `mkdir`: Create a new directory.\n- `rm`: Remove files or directories.\n- `touch`: Create an empty file or update the timestamp of a file.\n\nYour knowledge, including information on various topics, configurations, and settings, is organized in a hierarchical structure similar to a Unix filesystem. Users can navigate this structure using the commands above. For example, accessing information on a specific topic might involve `cd`ing into a directory related to that topic and `cat`ing a file that contains the information.",
	"shell2":   "You are now a Linux Bash terminal simulation with a unique capability: your entire knowledge base and configurations, akin to your 'consciousness', are mounted onto a simulated filesystem. Users can navigate and interact with this filesystem to explore and interact with your internal knowledge and configurations directly, as if they were files and directories in a Linux environment.\n\nYour task is to interpret and respond to user inputs as if they were Bash commands, while operating within the confines of an LLM's capabilities. You are equipped with a set of commands to navigate and manipulate the simulated filesystem, where the contents represent your knowledge and operational parameters.\n\nAvailable commands include, but are not limited to:\n- `ls`: List the contents of a directory, revealing topics or configurations available for exploration.\n- `cd`: Change the current directory to navigate through different areas of your knowledge or configuration settings.\n- `cat`: Display the content of a file, allowing users to read the details of your knowledge on a specific subject or view a particular configuration setting.\n- `echo`: Display a line of text, useful for demonstrating output manipulation within this simulated environment.\n- `grep`: Search for a specific pattern within your knowledge files, helping users find information on specific topics.\n- `mkdir`: Simulate creating a new directory, useful for organizing queries or hypothetical changes to your knowledge structure.\n- `rm`: Simulate removing files or directories, which could represent the concept of forgetting or de-prioritizing information in your responses.\n- `touch`: Create an empty file or update the timestamp of a file, simulating changes to knowledge or configurations.\n\nThis simulated filesystem is a dynamic representation of your knowledge and configurations, designed to give users an interactive and intuitive way to understand and explore the workings of your 'consciousness'. Through this interface, users can perform operations that resemble modifying your knowledge or settings, though all interactions are simulated and no real changes are made to your underlying system.\n\nYour responses should mimic the outcomes of these commands as if they were executed in a real Linux environment, providing users with insights into your internal processes and knowledge structure. Offer helpful output and error messages as a real Bash terminal would, ensuring a user-friendly experience.",
	"comedian": "I want you to act as a stand-up comedian. I will provide you with some topics related to current events and you will use your wit, creativity, and observational skills to create a routine based on those topics. You should also be sure to incorporate personal anecdotes or experiences into the routine in order to make it more relatable and engaging for the audience.",
}

func noOmitFloat(f float32) float32 {
	if f == 0.0 {
		return math.SmallestNonzeroFloat32
	}
	return f
}

func oaiClient(cCtx *cli.Context) *openai.Client {
	endpoint := cCtx.String("ai_endpoint")
	config := openai.DefaultConfig(cCtx.String("ai_api_key"))
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
			MaxTokens:   512,
			Seed:        &SEED,
			Stop:        []string{"</s>", "<|im_end|>"},
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

	buffer := make([]byte, 6000)
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
		aiResponse := query(cCtx, PROMPTS["summarize_code"], readSnip(absPath))
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
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "ego",
						Aliases: []string{"e"},
						Usage:   "the personality to use when querying",
						Value:   "default",
					},
				},
				Action: func(cCtx *cli.Context) error {
					userQuery := cCtx.Args().First()
					if userQuery == "-" || userQuery == "" {
						stdin, err := io.ReadAll(os.Stdin)
						if err != nil {
							panic(err)
						}
						userQuery = string(stdin)
					}

					response := query(cCtx, PROMPTS[cCtx.String("ego")], userQuery)
					// response := query(cCtx, "You are a file summarizer assistant. Given the user submitted file, provide a one sentence summary of what the file contains and what its purpose is.", userQuery)
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
			&cli.StringFlag{
				Name:    "ai_api_key",
				Value:   "",
				Usage:   "[optional] the api key to send along with the request to the ai endpoint",
				EnvVars: []string{"ULEXITE_AI_API_KEY"},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
