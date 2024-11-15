package cmd

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/michielnijenhuis/cli"
	"github.com/michielnijenhuis/cli/terminal"
)

const VERSION = "v1.0.0"

func Execute() {
	var rootCmd = &cli.Command{
		Name:               "concurrent",
		Version:            VERSION,
		NativeFlags:        []string{"help", "version"},
		CascadeNativeFlags: true,
		CatchErrors:        true,
		AutoExit:           true,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "colors",
				Shortcuts:   []string{"c"},
				Description: "Comma separated list of colors to identify the tails.",
			},
			&cli.StringFlag{
				Name:        "names",
				Shortcuts:   []string{"n"},
				Description: "Comma separated list of names to identify the tails.",
			},
		},
		Arguments: []cli.Arg{
			&cli.ArrayArg{
				Name:        "commands",
				Description: "List of commands to run tail concurrently.",
				Min:         1,
			},
		},
		RunE: func(io *cli.IO) error {
			colorsString := io.String("colors")
			colorsArray := strings.Split(colorsString, ",")
			namesString := io.String("names")
			namesArray := strings.Split(namesString, ",")

			commands := io.Array("commands")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
			defer func() {
				signal.Ignore(syscall.SIGINT)
			}()
			defer func() {
				signal.Stop(sigChan)
			}()

			names := make([]string, len(commands))
			colors := make([]string, len(commands))

			for i := range commands {
				clr := ""
				name := fmt.Sprintf("%d", i+1)

				if i < len(colorsArray) && colorsArray[i] != "" {
					clr = fmt.Sprintf("fg=%s", colorsArray[i])
				}

				if i < len(namesArray) && namesArray[i] != "" {
					name = namesArray[i]
				}

				colors[i] = clr
				names[i] = name
			}

			width := terminal.Columns()
			tagWidth := 0
			for _, name := range names {
				tagWidth = max(tagWidth, 3+len(name)) // "[name] "
			}
			columns := width - tagWidth

			var wg sync.WaitGroup

			go func(cancel context.CancelFunc) {
				<-sigChan
				io.NewLine(1)
				cancel()
			}(cancel)

			for i, command := range commands {
				wg.Add(1)

				go func(i int, command string, name string, color string, ctx context.Context, io *cli.IO) {
					defer wg.Done()

					args := cli.StringToInputArgs(command)
					process := exec.Command(args[0], args[1:]...)
					env := os.Environ()
					env = append(env, fmt.Sprintf("FORCE_COLOR=1"))
					env = append(env, fmt.Sprintf("COLUMNS=%d", columns))
					process.Env = env

					stdout, err := process.StdoutPipe()
					if err != nil {
						io.Err(err)
						return
					}

					if err := process.Start(); err != nil {
						io.Err(err)
						return
					}

					name = "[" + name + "]" + strings.Repeat(" ", tagWidth-(len(name)+3))

					cancelled := false

					tag := name
					if color != "" {
						tag = fmt.Sprintf("<%s>%s</>", color, name)
					}

					go func() {
						<-ctx.Done()
						io.Writelnf("%s %s exited", tag, command)
						if process.Process != nil {
							process.Process.Kill()
						}

						cancelled = true
					}()

					reader := bufio.NewReader(stdout)
					for {
						select {
						case <-ctx.Done():
							return
						default:
							line, err := reader.ReadBytes('\n')
							if err != nil {
								if err.Error() != "EOF" {
									io.Err(err)
								}

								if !cancelled {
									io.Writelnf("%s done", tag)
								}

								return
							}

							io.Writef("%s %s", tag, line)
						}
					}
				}(i, command, names[i], colors[i], ctx, io)
			}

			wg.Wait()

			return nil
		},
	}

	if err := rootCmd.Execute(); err != nil {
		log.Fatalln(err)
	}
}
