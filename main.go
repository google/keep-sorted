// Copyright 2023 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// keep-sorted is a tool that sorts lines between two markers in a larger file.
package main

import (
	"fmt"
	"os"
	"path"
	"time"

	"github.com/google/keep-sorted/cmd"
	"github.com/mattn/go-isatty"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	flag "github.com/spf13/pflag"
)

func main() {
	c := &cmd.Config{}
	c.FromFlags(nil)
	logLevel := flag.CountP("verbose", "v", "Log more verbosely")
	colorMode := flag.String("color", "auto", "Whether to color debug output. One of \"always\", \"never\", or \"auto\"")
	omitTimestamps := flag.Bool("omit-timestamps", false, "Do not emit timestamps in console logging. Useful for tests")
	if err := flag.CommandLine.MarkHidden("omit-timestamps"); err != nil {
		panic(err)
	}

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] file1 [file2 ...]\n\n", path.Base(os.Args[0]))
		fmt.Fprint(os.Stderr, "Note that '-' can be used to read from stdin, "+
			"in which case the output is written to stdout.\n\n")
		fmt.Fprintln(os.Stderr, "Flags:")
		flag.PrintDefaults()
	}

	flag.Parse()

	out := os.Stderr
	var shouldColor bool
	switch *colorMode {
	case "always":
		shouldColor = true
	case "never":
		shouldColor = false
	case "auto":
		shouldColor = isatty.IsTerminal(out.Fd())
	default:
		log.Err(fmt.Errorf("invalid --color %q", *colorMode)).Msg("")
	}
	cw := zerolog.ConsoleWriter{Out: out, TimeFormat: time.RFC3339, NoColor: !shouldColor}
	if *omitTimestamps {
		cw.FormatTimestamp = func(any) string { return "" }
	}
	log.Logger = log.Output(cw)
	zerolog.SetGlobalLevel(zerolog.Level(int(zerolog.WarnLevel) - *logLevel))
	if ok, err := cmd.Run(c, flag.Args()); err != nil {
		log.Fatal().AnErr("error", err).Msg("")
	} else if !ok {
		os.Exit(1)
	}
}
