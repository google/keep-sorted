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
	"errors"
	"fmt"
	"os"
	"path"
	"runtime/debug"
	"time"

	"github.com/google/keep-sorted/cmd"
	"github.com/mattn/go-isatty"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	flag "github.com/spf13/pflag"
)

func main() {
	flag.CommandLine.Init(os.Args[0], flag.ContinueOnError)

	c := &cmd.Config{}
	c.FromFlags(nil)
	logLevel := flag.CountP("verbose", "v", "Log more verbosely")
	colorMode := flag.String("color", "auto", "Whether to color debug output. One of \"always\", \"never\", or \"auto\"")
	omitTimestamps := flag.Bool("omit-timestamps", false, "Do not emit timestamps in console logging. Useful for tests")
	version := flag.Bool("version", false, "Report the keep-sorted version.")
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

	if err := flag.CommandLine.Parse(os.Args[1:]); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(2)
	}

	if *version {
		fmt.Fprintln(os.Stdout, readVersion())
		return
	}

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

func readVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}

	const revisionKey = "vcs.revision"
	const timeKey = "vcs.time"
	const dirtyKey = "vcs.modified"
	settings := make(map[string]string)
	for _, s := range bi.Settings {
		settings[s.Key] = s.Value
	}

	var s string
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		s = v
	} else if r := settings[revisionKey]; r != "" {
		s = r
		if len(s) > 7 {
			s = s[:7]
		}
	}

	if s == "" {
		return "unknown"
	}

	if settings[dirtyKey] == "true" {
		s += "-dev"
	}
	if t := settings[timeKey]; t != "" {
		if ts, err := time.Parse(time.RFC3339, t); err == nil {
			t = ts.In(time.Local).Format(time.RFC3339)
		}
		s += fmt.Sprintf(" (%s)", t)
	}
	return s
}
