package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"
)

type doctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // pass|warn|fail
	Message string `json:"message"`
	Tip     string `json:"tip,omitempty"`
}

type doctorReport struct {
	OK        bool          `json:"ok"`
	CheckedAt string        `json:"checkedAt"`
	Checks    []doctorCheck `json:"checks"`
}

func cmdDoctor(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", false, "output JSON")
	plain := fs.Bool("plain", false, "plain output")
	if err := fs.Parse(args); err != nil {
		exitCode(exitUsage)
	}
	report := runDoctorChecks(ctx)
	if *jsonOut {
		writeJSON(report)
	} else {
		printDoctorReport(report, *plain)
	}
	if !report.OK {
		exitCode(exitGeneric)
	}
}

func runDoctorChecks(ctx context.Context) doctorReport {
	report := doctorReport{
		OK:        true,
		CheckedAt: time.Now().Format(time.RFC3339),
	}
	add := func(c doctorCheck) {
		if c.Status == "fail" {
			report.OK = false
		}
		report.Checks = append(report.Checks, c)
	}

	if _, err := lookPath("osascript"); err != nil {
		add(doctorCheck{Name: "osascript", Status: "fail", Message: "osascript not found", Tip: "Install/restore macOS command-line tools."})
	} else {
		add(doctorCheck{Name: "osascript", Status: "pass", Message: "osascript available"})
	}
	if _, err := lookPath("shortcuts"); err != nil {
		add(doctorCheck{Name: "shortcuts", Status: "warn", Message: "shortcuts command not found", Tip: "Native backend requires the Shortcuts CLI."})
	} else {
		add(doctorCheck{Name: "shortcuts", Status: "pass", Message: "shortcuts available"})
	}

	path, err := configPath()
	if err != nil {
		add(doctorCheck{Name: "config-path", Status: "fail", Message: fmt.Sprintf("cannot resolve config path: %v", err)})
	} else {
		add(doctorCheck{Name: "config-path", Status: "pass", Message: path})
		cfg, cfgErr := loadConfigOptional()
		if cfgErr != nil {
			add(doctorCheck{Name: "config", Status: "fail", Message: cfgErr.Error(), Tip: "Fix JSON syntax or re-run `homepodctl config-init`."})
		} else if len(cfg.Aliases) == 0 {
			add(doctorCheck{Name: "config", Status: "warn", Message: "no aliases configured", Tip: "Run `homepodctl config-init` and edit defaults/aliases."})
		} else {
			add(doctorCheck{Name: "config", Status: "pass", Message: fmt.Sprintf("aliases=%d", len(cfg.Aliases))})
		}
	}

	backendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := getNowPlaying(backendCtx); err != nil {
		add(doctorCheck{
			Name:    "music-backend",
			Status:  "warn",
			Message: formatError(err),
			Tip:     "Open Music.app and grant Automation permissions if prompted.",
		})
	} else {
		add(doctorCheck{Name: "music-backend", Status: "pass", Message: "Music backend reachable"})
	}
	return report
}

func printDoctorReport(report doctorReport, plain bool) {
	if plain {
		fmt.Println("STATUS\tCHECK\tMESSAGE\tTIP")
		for _, c := range report.Checks {
			fmt.Printf("%s\t%s\t%s\t%s\n", c.Status, c.Name, c.Message, c.Tip)
		}
		return
	}
	fmt.Printf("doctor ok=%t checked_at=%s\n", report.OK, report.CheckedAt)
	for _, c := range report.Checks {
		if c.Tip != "" {
			fmt.Printf("%s\t%s\t%s (tip: %s)\n", c.Status, c.Name, c.Message, c.Tip)
			continue
		}
		fmt.Printf("%s\t%s\t%s\n", c.Status, c.Name, c.Message)
	}
}
