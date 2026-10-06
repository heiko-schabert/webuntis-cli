// Command webuntis-cli reads WebUntis timetables, homework, exams and
// absences from the terminal and, with the mcp subcommand, serves the same
// tools over MCP.
package main

import (
	"cmp"
	"context"
	"log/slog"
	"os"
	"time"
	"webuntis-cli/untis"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type none struct{}

type loginStatus struct {
	Status string `json:"status"`
}

type childArg struct {
	Child string `json:"child,omitempty" jsonschema:"child's first name; only needed with several children"`
}

type rangeArgs struct {
	childArg
	StartDate string `json:"start_date,omitempty" jsonschema:"YYYY-MM-DD, default today"`
	EndDate   string `json:"end_date,omitempty" jsonschema:"YYYY-MM-DD, default start + 4 days"`
}

type classArgs struct {
	rangeArgs
	ClassName string `json:"class_name,omitempty" jsonschema:"class name like 5a; default the child's own class"`
}

type daysArgs struct {
	childArg
	DaysAhead int `json:"days_ahead,omitempty" jsonschema:"days to look ahead"`
}

type absenceArgs struct {
	StartDate string `json:"start_date,omitempty" jsonschema:"YYYY-MM-DD, default school year start"`
	EndDate   string `json:"end_date,omitempty" jsonschema:"YYYY-MM-DD, default school year end"`
}

type dateArg struct {
	Date string `json:"date,omitempty" jsonschema:"YYYY-MM-DD, default today"`
}

type children struct {
	Children []untis.Child `json:"children"`
}

// tool hides the SDK's result plumbing.
func tool[In, Out any](s *mcp.Server, name, desc string, fn func(context.Context, In) (Out, error)) {
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: desc},
		func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
			start := time.Now()
			out, err := fn(ctx, in)
			slog.InfoContext(ctx, "tool call", "tool", name, "duration", time.Since(start).Round(time.Millisecond), "err", err)
			return nil, out, err
		})
}

func dateRange(in rangeArgs) (time.Time, time.Time, error) {
	start, err := untis.ParseDate(in.StartDate)
	if err != nil {
		return start, start, err
	}
	if in.EndDate == "" {
		return start, start.AddDate(0, 0, 4), nil
	}
	end, err := untis.ParseDate(in.EndDate)
	return start, end, err
}

// schoolDay is d, or the following Monday when d falls on a weekend.
func schoolDay(d time.Time) time.Time {
	for d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
		d = d.AddDate(0, 0, 1)
	}
	return d
}

func newServer(c *untis.Client) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "webuntis", Version: "v0.1.0"}, nil)
	day := func(ctx context.Context, child, title string, d time.Time) (untis.Timetable, error) {
		ls, err := c.Timetable(ctx, child, d, d)
		return untis.Timetable{Title: title + " " + d.Format("Monday 2006-01-02"), Lessons: ls}, err
	}
	tool(s, "check_login", "Checks that logging in to WebUntis works.",
		func(ctx context.Context, _ none) (loginStatus, error) {
			if err := c.CheckLogin(ctx); err != nil {
				return loginStatus{}, err
			}
			return loginStatus{Status: "ok"}, nil
		})
	tool(s, "list_children", "Children on the account (ID, name). Names for the child parameter of other tools.",
		func(ctx context.Context, _ none) (children, error) {
			k, err := c.Children(ctx)
			return children{k}, err
		})
	tool(s, "get_timetable", "Timetable of the child for a date range: per lesson date, time, subject, room, teacher and changes (cancelled, substitution, original room/teacher).",
		func(ctx context.Context, in rangeArgs) (untis.Timetable, error) {
			start, end, err := dateRange(in)
			if err != nil {
				return untis.Timetable{}, err
			}
			ls, err := c.Timetable(ctx, in.Child, start, end)
			return untis.Timetable{Title: "Timetable", Lessons: ls}, err
		})
	tool(s, "get_today", "Today's lessons including cancellations and substitutions.",
		func(ctx context.Context, in childArg) (untis.Timetable, error) {
			d, _ := untis.ParseDate("")
			return day(ctx, in.Child, "Today", d)
		})
	tool(s, "get_tomorrow", "Lessons of the next school day (Monday if tomorrow is a weekend).",
		func(ctx context.Context, in childArg) (untis.Timetable, error) {
			d, _ := untis.ParseDate("")
			return day(ctx, in.Child, "Next school day", schoolDay(d.AddDate(0, 0, 1)))
		})
	tool(s, "get_changes", "Only the changed lessons in a date range: cancellations, substitutions, room and teacher swaps.",
		func(ctx context.Context, in rangeArgs) (untis.Timetable, error) {
			start, end, err := dateRange(in)
			if err != nil {
				return untis.Timetable{}, err
			}
			ls, err := c.Timetable(ctx, in.Child, start, end)
			out := untis.Timetable{Title: "Changes", Lessons: []untis.Lesson{}}
			for _, l := range ls {
				if l.Changed() {
					out.Lessons = append(out.Lessons, l)
				}
			}
			return out, err
		})
	tool(s, "get_class_timetable", "Timetable of any class at the school, e.g. for religion or split groups. Class names come from list_classes.",
		func(ctx context.Context, in classArgs) (untis.Timetable, error) {
			start, end, err := dateRange(in.rangeArgs)
			if err != nil {
				return untis.Timetable{}, err
			}
			return c.ClassTimetable(ctx, in.Child, in.ClassName, start, end)
		})
	tool(s, "list_classes", "Active classes of the school this year.",
		func(ctx context.Context, in childArg) (untis.Classes, error) { return c.Classes(ctx, in.Child) })
	tool(s, "get_homework", "Homework assigned from today on (default 14 days ahead): subject, teacher, text, due date, completed.",
		func(ctx context.Context, in daysArgs) (untis.HomeworkList, error) {
			return c.Homework(ctx, in.Child, cmp.Or(in.DaysAhead, 14))
		})
	tool(s, "get_exams", "Exams from today on (default 30 days ahead): date, time, subject, type, text.",
		func(ctx context.Context, in daysArgs) (untis.Exams, error) {
			return c.Exams(ctx, in.Child, cmp.Or(in.DaysAhead, 30))
		})
	tool(s, "get_absences", "Registered absences of all children on the account (default current school year): date, time, reason, excuse status, note.",
		func(ctx context.Context, in absenceArgs) (untis.Absences, error) {
			var start, end time.Time
			var err error
			if in.StartDate != "" {
				if start, err = untis.ParseDate(in.StartDate); err != nil {
					return untis.Absences{}, err
				}
			}
			if in.EndDate != "" {
				if end, err = untis.ParseDate(in.EndDate); err != nil {
					return untis.Absences{}, err
				}
			}
			return c.Absences(ctx, start, end)
		})
	tool(s, "get_messages", "School messages of the day (announcements, notices).",
		func(ctx context.Context, in dateArg) (untis.Messages, error) {
			d, err := untis.ParseDate(in.Date)
			if err != nil {
				return untis.Messages{}, err
			}
			return c.Messages(ctx, d)
		})
	tool(s, "get_school_info", "School, school year, the child's class, period times, holidays and last data import.",
		func(ctx context.Context, in childArg) (untis.SchoolInfo, error) { return c.SchoolInfo(ctx, in.Child) })
	return s
}

func main() {
	cfg, err := untis.LoadConfig(os.Getenv, untis.DefaultEnvFile())
	ready := func() error { return err }
	os.Exit(run(context.Background(), newServer(untis.New(cfg)), ready, os.Args[1:], os.Stdout, os.Stderr))
}
