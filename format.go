package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"webuntis-cli/untis"
)

// formatters render a tool's structured result for humans; tools without one
// print JSON. MCP clients always get JSON.
var formatters = map[string]func(io.Writer, json.RawMessage) error{
	"check_login": textOf(func(w io.Writer, s loginStatus) { fmt.Fprintln(w, "login", s.Status) }),
	"list_children": textOf(func(w io.Writer, c children) {
		for _, k := range c.Children {
			fmt.Fprintln(w, strings.TrimSpace(k.FirstName+" "+k.LastName))
		}
	}),
	"get_timetable":       textOf(writeTimetable),
	"get_today":           textOf(writeTimetable),
	"get_tomorrow":        textOf(writeTimetable),
	"get_changes":         textOf(writeTimetable),
	"get_class_timetable": textOf(writeTimetable),
	"list_classes": textOf(func(w io.Writer, c untis.Classes) {
		for _, k := range c.Classes {
			fmt.Fprintf(w, "%-8s %s\n", k.Name, k.LongName)
		}
	}),
	"get_homework": textOf(func(w io.Writer, h untis.HomeworkList) {
		if len(h.Homework) == 0 {
			fmt.Fprintln(w, "No homework.")
		}
		for _, hw := range h.Homework {
			mark := " "
			if hw.Completed {
				mark = "✓"
			}
			fmt.Fprintf(w, "%s due %s  %-6s %s\n", mark, hw.Due, hw.Subject, oneLine(hw.Text))
		}
	}),
	"get_exams": textOf(func(w io.Writer, e untis.Exams) {
		if len(e.Exams) == 0 {
			fmt.Fprintln(w, "No exams.")
		}
		for _, x := range e.Exams {
			fmt.Fprintf(w, "%s %s %s-%s  %-6s %s\n", weekday(x.Date), x.Date, x.Start, x.End, x.Subject,
				strings.Join(nonEmpty(x.Type, x.Name, x.Text), " · "))
		}
	}),
	"get_absences": textOf(func(w io.Writer, a untis.Absences) {
		if len(a.Absences) == 0 {
			fmt.Fprintln(w, "No absences.")
		}
		for _, x := range a.Absences {
			fmt.Fprintf(w, "%s %s-%s  %s (%s)", x.StartDate, x.Start, x.End, x.Reason, x.Status)
			if x.Student != "" {
				fmt.Fprintf(w, "  %s", x.Student)
			}
			fmt.Fprintln(w)
			if x.Note != "" {
				fmt.Fprintf(w, "  %s\n", x.Note)
			}
		}
	}),
	"get_messages": textOf(func(w io.Writer, m untis.Messages) {
		if len(m.Messages) == 0 {
			fmt.Fprintf(w, "No messages on %s.\n", m.Date)
		}
		for _, x := range m.Messages {
			fmt.Fprintf(w, "[%s]\n", x.Subject)
			if x.Body != "" {
				fmt.Fprintln(w, x.Body)
			}
			if len(x.Attachments) > 0 {
				fmt.Fprintf(w, "Attachments: %s\n", strings.Join(x.Attachments, ", "))
			}
			fmt.Fprintln(w)
		}
	}),
	"get_school_info": textOf(func(w io.Writer, i untis.SchoolInfo) {
		fmt.Fprintf(w, "Student: %s", i.Student)
		if i.Class != "" {
			fmt.Fprintf(w, ", %s", i.Class)
		}
		fmt.Fprintf(w, "\nSchool: %s\nSchool year: %s\nLast data import: %s\n\nPeriods:\n", i.School, i.SchoolYear, i.LastImport)
		for _, p := range i.Periods {
			fmt.Fprintf(w, "  %2s  %s-%s\n", p.Number, p.Start, p.End)
		}
		if len(i.Holidays) > 0 {
			fmt.Fprintln(w, "\nHolidays:")
		}
		for _, h := range i.Holidays {
			if h.Start == h.End {
				fmt.Fprintf(w, "  %s              %s\n", h.Start, h.Name)
			} else {
				fmt.Fprintf(w, "  %s - %s  %s\n", h.Start, h.End, h.Name)
			}
		}
	}),
}

func format(w io.Writer, v any, f func(io.Writer, json.RawMessage) error) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return f(w, b)
}

// textOf decodes the tool result back into its Go type.
func textOf[T any](f func(io.Writer, T)) func(io.Writer, json.RawMessage) error {
	return func(w io.Writer, b json.RawMessage) error {
		var v T
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		f(w, v)
		return nil
	}
}

func writeTimetable(w io.Writer, t untis.Timetable) {
	fmt.Fprintln(w, t.Title)
	if len(t.Lessons) == 0 {
		fmt.Fprintln(w, "No lessons.")
	}
	date := ""
	for _, l := range t.Lessons {
		if l.Date != date {
			date = l.Date
			fmt.Fprintf(w, "\n%s %s\n", weekday(date), date)
		}
		fmt.Fprintf(w, "  %s-%s  %-8s %-10s %s", l.Start, l.End, l.Subject, l.Room, l.Teacher)
		var tags []string
		if l.Status != "" {
			tags = append(tags, strings.ToUpper(l.Status))
		}
		tags = append(tags, nonEmpty(l.Substitution, l.Info)...)
		if l.OriginalRoom != "" {
			tags = append(tags, "room was "+l.OriginalRoom)
		}
		if l.OriginalTeacher != "" {
			tags = append(tags, "teacher was "+l.OriginalTeacher)
		}
		if len(tags) > 0 {
			fmt.Fprintf(w, "  [%s]", oneLine(strings.Join(tags, ", ")))
		}
		fmt.Fprintln(w)
	}
}

func weekday(date string) string {
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		return "   "
	}
	return d.Weekday().String()[:3]
}

// oneLine keeps multi-line Untis texts from breaking the one-lesson-per-line layout.
func oneLine(s string) string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, " | ")
}

func nonEmpty(ss ...string) (out []string) {
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
