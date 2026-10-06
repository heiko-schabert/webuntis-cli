package untis

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

const day = "2006-01-02"

// ParseDate reads YYYY-MM-DD; empty means today.
func ParseDate(s string) (time.Time, error) {
	if s == "" {
		y, m, d := time.Now().Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.Local), nil
	}
	t, err := time.ParseInLocation(day, s, time.Local)
	if err != nil {
		return t, fmt.Errorf("date %q: want YYYY-MM-DD", s)
	}
	return t, nil
}

func ymdInt(t time.Time) int {
	n, _ := strconv.Atoi(t.Format("20060102"))
	return n
}

// ymd decodes dates Untis sends either as 20260911 or "2026-09-11".
type ymd string

func (d *ymd) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if len(s) == 8 && !strings.Contains(s, "-") {
		s = s[:4] + "-" + s[4:6] + "-" + s[6:]
	}
	*d = ymd(s)
	return nil
}

// hhmm decodes times sent as "T08:00", "08:00" or 800.
type hhmm string

func (h *hhmm) UnmarshalJSON(b []byte) error {
	s := strings.TrimPrefix(strings.Trim(string(b), `"`), "T")
	if !strings.Contains(s, ":") {
		s = fmt.Sprintf("%04s", s)
		s = s[:2] + ":" + s[2:]
	}
	*h = hhmm(s)
	return nil
}

// stamp splits Untis datetimes like "2026-09-11T08:55Z". The Z is a lie:
// the value is school-local wall time, so it is not converted.
type stamp struct{ Date, Time string }

func (s *stamp) UnmarshalJSON(b []byte) error {
	v := strings.TrimSuffix(strings.Trim(string(b), `"`), "Z")
	s.Date, s.Time, _ = strings.Cut(v, "T")
	if len(s.Time) > 5 {
		s.Time = s.Time[:5]
	}
	return nil
}

type element struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	LongName string `json:"longName"`
	Active   *bool  `json:"active"`
}

type span struct {
	Name      string `json:"name"`
	LongName  string `json:"longName"`
	StartDate ymd    `json:"startDate"`
	EndDate   ymd    `json:"endDate"`
}

type masterData struct {
	TimeStamp   int64     `json:"timeStamp"`
	Subjects    []element `json:"subjects"`
	Teachers    []element `json:"teachers"`
	Rooms       []element `json:"rooms"`
	Klassen     []element `json:"klassen"`
	Holidays    []span    `json:"holidays"`
	Schoolyears []span    `json:"schoolyears"`
	TimeGrid    struct {
		Days []struct {
			Units []struct {
				Label     string `json:"label"`
				StartTime hhmm   `json:"startTime"`
				EndTime   hhmm   `json:"endTime"`
			} `json:"units"`
		} `json:"days"`
	} `json:"timeGrid"`
}

func find(es []element, id int) element {
	for _, e := range es {
		if e.ID == id {
			return e
		}
	}
	return element{}
}

type rawPeriod struct {
	ID       int      `json:"id"`
	Start    stamp    `json:"startDateTime"`
	End      stamp    `json:"endDateTime"`
	Is       []string `json:"is"`
	SG       string   `json:"sg"`
	Elements []struct {
		Type  string `json:"type"`
		ID    int    `json:"id"`
		OrgID int    `json:"orgId"`
	} `json:"elements"`
	Text struct {
		Lesson       string `json:"lesson"`
		Substitution string `json:"substitution"`
		Info         string `json:"info"`
	} `json:"text"`
}

// timetable fetches periods and keeps the master data that comes with them.
func (c *Client) timetable(ctx context.Context, id int, typ string, start, end time.Time) ([]rawPeriod, *masterData, error) {
	c.mu.Lock()
	ts := c.masterTS
	c.mu.Unlock()
	var r struct {
		MasterData masterData `json:"masterData"`
		Timetable  struct {
			Periods []rawPeriod `json:"periods"`
		} `json:"timetable"`
	}
	err := c.rpc(ctx, "getTimetable2017", map[string]any{
		"id": id, "type": typ, "startDate": ymdInt(start), "endDate": ymdInt(end),
		"masterDataTimestamp": ts, "timetableTimestamp": 0, "timetableTimestamps": []int{},
	}, &r)
	if err != nil {
		return nil, nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// Untis only resends master data when it changed since ts.
	if len(r.MasterData.Subjects) > 0 {
		c.master, c.masterTS = &r.MasterData, r.MasterData.TimeStamp
	}
	return r.Timetable.Periods, c.master, nil
}

func (c *Client) masterData(ctx context.Context, kid Child) (*masterData, error) {
	c.mu.Lock()
	md := c.master
	c.mu.Unlock()
	if md != nil {
		return md, nil
	}
	today, _ := ParseDate("")
	_, md, err := c.timetable(ctx, kid.ID, "STUDENT", today, today)
	if err == nil && md == nil {
		err = fmt.Errorf("WebUntis sent no master data")
	}
	return md, err
}

// Lesson is one timetable period. Status is cancelled, irregular or empty.
type Lesson struct {
	Date            string `json:"date"`
	Start           string `json:"start"`
	End             string `json:"end"`
	Subject         string `json:"subject"`
	SubjectLong     string `json:"subject_long,omitempty"`
	Room            string `json:"room,omitempty"`
	Teacher         string `json:"teacher,omitempty"`
	Class           string `json:"class,omitempty"`
	Status          string `json:"status,omitempty"`
	Substitution    string `json:"substitution,omitempty"`
	LessonText      string `json:"lesson_text,omitempty"`
	Info            string `json:"info,omitempty"`
	Group           string `json:"group,omitempty"`
	OriginalRoom    string `json:"original_room,omitempty"`
	OriginalTeacher string `json:"original_teacher,omitempty"`
}

// Changed reports cancellations, substitutions and swaps.
func (l Lesson) Changed() bool {
	return l.Status != "" || l.Substitution != "" || l.OriginalRoom != "" || l.OriginalTeacher != ""
}

type Timetable struct {
	Title   string   `json:"title"`
	Lessons []Lesson `json:"lessons"`
}

func lessons(ps []rawPeriod, md *masterData) []Lesson {
	if md == nil {
		md = &masterData{}
	}
	out := []Lesson{}
	for _, p := range ps {
		l := Lesson{Date: p.Start.Date, Start: p.Start.Time, End: p.End.Time, Group: p.SG,
			Substitution: p.Text.Substitution, LessonText: p.Text.Lesson, Info: p.Text.Info}
		for _, e := range p.Elements {
			switch e.Type {
			case "SUBJECT":
				s := find(md.Subjects, e.ID)
				l.Subject, l.SubjectLong = s.Name, s.LongName
			case "TEACHER":
				l.Teacher = find(md.Teachers, e.ID).Name
				if e.OrgID != 0 && e.OrgID != e.ID {
					l.OriginalTeacher = find(md.Teachers, e.OrgID).Name
				}
			case "ROOM":
				l.Room = find(md.Rooms, e.ID).Name
				if e.OrgID != 0 && e.OrgID != e.ID {
					l.OriginalRoom = find(md.Rooms, e.OrgID).Name
				}
			case "CLASS":
				l.Class = find(md.Klassen, e.ID).Name
			}
		}
		switch {
		case slices.Contains(p.Is, "CANCELLED"):
			l.Status = "cancelled"
		case slices.Contains(p.Is, "IRREGULAR"):
			l.Status = "irregular"
		}
		out = append(out, l)
	}
	slices.SortFunc(out, func(a, b Lesson) int { return cmp.Or(cmp.Compare(a.Date, b.Date), cmp.Compare(a.Start, b.Start)) })
	return out
}

// Timetable returns the child's lessons from start to end inclusive.
func (c *Client) Timetable(ctx context.Context, child string, start, end time.Time) ([]Lesson, error) {
	kid, err := c.child(ctx, child)
	if err != nil {
		return nil, err
	}
	ps, md, err := c.timetable(ctx, kid.ID, "STUDENT", start, end)
	return lessons(ps, md), err
}

// studentClass finds the child's class from the coming school week's lessons.
func (c *Client) studentClass(ctx context.Context, kid Child) (element, error) {
	d, _ := ParseDate("")
	for d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
		d = d.AddDate(0, 0, 1)
	}
	ps, md, err := c.timetable(ctx, kid.ID, "STUDENT", d, d.AddDate(0, 0, 4))
	if err != nil {
		return element{}, err
	}
	for _, p := range ps {
		for _, e := range p.Elements {
			if e.Type == "CLASS" && md != nil {
				if k := find(md.Klassen, e.ID); k.ID != 0 {
					return k, nil
				}
			}
		}
	}
	return element{}, fmt.Errorf("could not determine the class of %s, pass class_name", kid.FirstName)
}

// ClassTimetable returns any class's lessons; empty name means the child's class.
func (c *Client) ClassTimetable(ctx context.Context, child, name string, start, end time.Time) (Timetable, error) {
	kid, err := c.child(ctx, child)
	if err != nil {
		return Timetable{}, err
	}
	var k element
	if name = strings.TrimSpace(name); name == "" {
		k, err = c.studentClass(ctx, kid)
	} else {
		k, err = c.class(ctx, kid, name)
	}
	if err != nil {
		return Timetable{}, err
	}
	ps, md, err := c.timetable(ctx, k.ID, "CLASS", start, end)
	return Timetable{Title: "Class " + k.Name, Lessons: lessons(ps, md)}, err
}

func (c *Client) class(ctx context.Context, kid Child, name string) (element, error) {
	md, err := c.masterData(ctx, kid)
	if err != nil {
		return element{}, err
	}
	var names []string
	for _, k := range md.Klassen {
		if strings.EqualFold(k.Name, name) {
			return k, nil
		}
		names = append(names, k.Name)
	}
	slices.Sort(names)
	return element{}, fmt.Errorf("class %q not found, available: %s", name, strings.Join(slices.Compact(names), ", "))
}

type Class struct {
	Name     string `json:"name"`
	LongName string `json:"long_name,omitempty"`
}

type Classes struct {
	Classes []Class `json:"classes"`
}

// Classes lists the active classes of the school.
func (c *Client) Classes(ctx context.Context, child string) (Classes, error) {
	kid, err := c.child(ctx, child)
	if err != nil {
		return Classes{}, err
	}
	md, err := c.masterData(ctx, kid)
	if err != nil {
		return Classes{}, err
	}
	out := Classes{Classes: []Class{}}
	for _, k := range md.Klassen {
		if k.Active == nil || *k.Active {
			out.Classes = append(out.Classes, Class{k.Name, k.LongName})
		}
	}
	slices.SortFunc(out.Classes, func(a, b Class) int { return cmp.Compare(a.Name, b.Name) })
	return out, nil
}

type Homework struct {
	Assigned  string `json:"assigned"`
	Due       string `json:"due"`
	Subject   string `json:"subject"`
	Teacher   string `json:"teacher,omitempty"`
	Text      string `json:"text"`
	Completed bool   `json:"completed"`
}

type HomeworkList struct {
	Homework []Homework `json:"homework"`
}

// Homework lists assignments given from today to days ahead.
func (c *Client) Homework(ctx context.Context, child string, days int) (HomeworkList, error) {
	kid, err := c.child(ctx, child)
	if err != nil {
		return HomeworkList{}, err
	}
	md, err := c.masterData(ctx, kid)
	if err != nil {
		return HomeworkList{}, err
	}
	start, _ := ParseDate("")
	type lesson struct {
		ID         int    `json:"id"`
		Subject    string `json:"subject"`
		SubjectID  int    `json:"subjectId"`
		TeacherIDs []int  `json:"teacherIds"`
	}
	// Untis has sent lessons both as a list and keyed by id.
	var r struct {
		HomeWorks []struct {
			LessonID  int    `json:"lessonId"`
			StartDate ymd    `json:"startDate"`
			EndDate   ymd    `json:"endDate"`
			Text      string `json:"text"`
			Completed bool   `json:"completed"`
		} `json:"homeWorks"`
		Lessons     []lesson          `json:"lessons"`
		LessonsByID map[string]lesson `json:"lessonsById"`
	}
	if err := c.rpc(ctx, "getHomeWork2017", map[string]any{
		"id": kid.ID, "type": "STUDENT", "startDate": ymdInt(start), "endDate": ymdInt(start.AddDate(0, 0, days)),
	}, &r); err != nil {
		return HomeworkList{}, err
	}
	for _, l := range r.Lessons {
		if r.LessonsByID == nil {
			r.LessonsByID = map[string]lesson{}
		}
		r.LessonsByID[strconv.Itoa(l.ID)] = l
	}
	out := HomeworkList{Homework: []Homework{}}
	for _, h := range r.HomeWorks {
		l := r.LessonsByID[strconv.Itoa(h.LessonID)]
		hw := Homework{Assigned: string(h.StartDate), Due: string(h.EndDate), Subject: l.Subject, Text: h.Text, Completed: h.Completed}
		if s := find(md.Subjects, l.SubjectID).Name; s != "" {
			hw.Subject = s
		}
		if len(l.TeacherIDs) > 0 {
			hw.Teacher = find(md.Teachers, l.TeacherIDs[0]).Name
		}
		out.Homework = append(out.Homework, hw)
	}
	slices.SortFunc(out.Homework, func(a, b Homework) int { return cmp.Compare(a.Due, b.Due) })
	return out, nil
}

type Exam struct {
	Date     string   `json:"date"`
	Start    string   `json:"start"`
	End      string   `json:"end"`
	Subject  string   `json:"subject,omitempty"`
	Type     string   `json:"type,omitempty"`
	Name     string   `json:"name,omitempty"`
	Text     string   `json:"text,omitempty"`
	Teachers []string `json:"teachers,omitempty"`
	Rooms    []string `json:"rooms,omitempty"`
}

type Exams struct {
	Exams []Exam `json:"exams"`
}

// Exams lists exams from today to days ahead.
func (c *Client) Exams(ctx context.Context, child string, days int) (Exams, error) {
	kid, err := c.child(ctx, child)
	if err != nil {
		return Exams{}, err
	}
	md, err := c.masterData(ctx, kid)
	if err != nil {
		return Exams{}, err
	}
	start, _ := ParseDate("")
	var r struct {
		Exams []struct {
			ExamType   string `json:"examType"`
			Start      stamp  `json:"startDateTime"`
			End        stamp  `json:"endDateTime"`
			SubjectID  int    `json:"subjectId"`
			TeacherIDs []int  `json:"teacherIds"`
			RoomIDs    []int  `json:"roomIds"`
			Name       string `json:"name"`
			Text       string `json:"text"`
		} `json:"exams"`
	}
	if err := c.rpc(ctx, "getExams2017", map[string]any{
		"id": kid.ID, "type": "STUDENT", "startDate": ymdInt(start), "endDate": ymdInt(start.AddDate(0, 0, days)),
	}, &r); err != nil {
		return Exams{}, err
	}
	names := func(es []element, ids []int) (out []string) {
		for _, id := range ids {
			out = append(out, find(es, id).Name)
		}
		return out
	}
	out := Exams{Exams: []Exam{}}
	for _, e := range r.Exams {
		out.Exams = append(out.Exams, Exam{Date: e.Start.Date, Start: e.Start.Time, End: e.End.Time,
			Subject: find(md.Subjects, e.SubjectID).Name, Type: e.ExamType, Name: e.Name, Text: e.Text,
			Teachers: names(md.Teachers, e.TeacherIDs), Rooms: names(md.Rooms, e.RoomIDs)})
	}
	slices.SortFunc(out.Exams, func(a, b Exam) int { return cmp.Or(cmp.Compare(a.Date, b.Date), cmp.Compare(a.Start, b.Start)) })
	return out, nil
}

type Absence struct {
	Student   string `json:"student,omitempty"`
	StartDate string `json:"start_date"`
	Start     string `json:"start"`
	EndDate   string `json:"end_date"`
	End       string `json:"end"`
	Reason    string `json:"reason"`
	Excused   bool   `json:"excused"`
	Status    string `json:"status"`
	Note      string `json:"note,omitempty"`
}

type Absences struct {
	Absences []Absence `json:"absences"`
}

// Absences lists absences between start and end; zero times mean the
// current school year. Untis returns them for every child on the account.
func (c *Client) Absences(ctx context.Context, start, end time.Time) (Absences, error) {
	if start.IsZero() || end.IsZero() {
		kids, err := c.Children(ctx)
		if err != nil {
			return Absences{}, err
		}
		md, err := c.masterData(ctx, kids[0])
		if err != nil {
			return Absences{}, err
		}
		sy := schoolYear(md)
		if start.IsZero() {
			start, _ = ParseDate(string(sy.StartDate))
		}
		if end.IsZero() {
			end, _ = ParseDate(string(sy.EndDate))
		}
	}
	var r struct {
		Absences []struct {
			StudentName string `json:"studentName"`
			Start       stamp  `json:"startDateTime"`
			End         stamp  `json:"endDateTime"`
			Reason      string `json:"absenceReason"`
			Text        string `json:"text"`
			Excused     bool   `json:"excused"`
			Excuse      *struct {
				Text string `json:"text"`
			} `json:"excuse"`
		} `json:"absences"`
	}
	if err := c.rpc(ctx, "getStudentAbsences2017", map[string]any{
		"startDate": ymdInt(start), "endDate": ymdInt(end), "includeExcused": true, "includeUnExcused": true,
	}, &r); err != nil {
		return Absences{}, err
	}
	out := Absences{Absences: []Absence{}}
	for _, a := range r.Absences {
		status := "not excused"
		switch {
		case a.Excused:
			status = "excused"
		case a.Excuse != nil && a.Excuse.Text != "":
			status = a.Excuse.Text
		}
		out.Absences = append(out.Absences, Absence{Student: a.StudentName, StartDate: a.Start.Date, Start: a.Start.Time,
			EndDate: a.End.Date, End: a.End.Time, Reason: a.Reason, Excused: a.Excused, Status: status, Note: a.Text})
	}
	slices.SortFunc(out.Absences, func(a, b Absence) int {
		return cmp.Or(cmp.Compare(a.StartDate, b.StartDate), cmp.Compare(a.Start, b.Start))
	})
	return out, nil
}

// schoolYear picks the year containing today, else the last one.
func schoolYear(md *masterData) span {
	today := time.Now().Format(day)
	for _, sy := range md.Schoolyears {
		if string(sy.StartDate) <= today && today <= string(sy.EndDate) {
			return sy
		}
	}
	if n := len(md.Schoolyears); n > 0 {
		return md.Schoolyears[n-1]
	}
	return span{}
}

type Message struct {
	Subject     string   `json:"subject"`
	Body        string   `json:"body,omitempty"`
	Attachments []string `json:"attachments,omitempty"`
}

type Messages struct {
	Date     string    `json:"date"`
	Messages []Message `json:"messages"`
}

// Messages returns the school's messages of the day.
func (c *Client) Messages(ctx context.Context, d time.Time) (Messages, error) {
	var r struct {
		Messages []struct {
			Subject     string            `json:"subject"`
			Body        string            `json:"body"`
			Attachments []json.RawMessage `json:"attachments"`
		} `json:"messages"`
	}
	if err := c.rpc(ctx, "getMessagesOfDay2017", map[string]any{"date": ymdInt(d)}, &r); err != nil {
		return Messages{}, err
	}
	out := Messages{Date: d.Format(day), Messages: []Message{}}
	for _, m := range r.Messages {
		msg := Message{Subject: m.Subject, Body: strings.TrimSpace(m.Body)}
		for _, a := range m.Attachments {
			var v struct{ Name, URL string }
			if json.Unmarshal(a, &v) != nil {
				json.Unmarshal(a, &v.Name) // plain string
			}
			msg.Attachments = append(msg.Attachments, cmp.Or(v.Name, v.URL))
		}
		out.Messages = append(out.Messages, msg)
	}
	return out, nil
}

type Period struct {
	Number string `json:"number"`
	Start  string `json:"start"`
	End    string `json:"end"`
}

type Holiday struct {
	Name  string `json:"name"`
	Start string `json:"start"`
	End   string `json:"end"`
}

type SchoolInfo struct {
	School     string    `json:"school"`
	SchoolYear string    `json:"school_year"`
	Student    string    `json:"student"`
	Class      string    `json:"class,omitempty"`
	LastImport string    `json:"last_import,omitempty"`
	Periods    []Period  `json:"periods"`
	Holidays   []Holiday `json:"holidays"`
}

// SchoolInfo returns period times, this school year's holidays and the child's class.
func (c *Client) SchoolInfo(ctx context.Context, child string) (SchoolInfo, error) {
	kid, err := c.child(ctx, child)
	if err != nil {
		return SchoolInfo{}, err
	}
	md, err := c.masterData(ctx, kid)
	if err != nil {
		return SchoolInfo{}, err
	}
	u, err := c.userData(ctx)
	if err != nil {
		return SchoolInfo{}, err
	}
	sy := schoolYear(md)
	info := SchoolInfo{School: cmp.Or(u.SchoolName, c.cfg.School), SchoolYear: sy.Name, Student: kid.FirstName,
		Periods: []Period{}, Holidays: []Holiday{}}
	// Class is a nicety; holidays have no lessons to read it from.
	if k, err := c.studentClass(ctx, kid); err == nil {
		info.Class = k.Name
	}
	if md.TimeStamp > 0 {
		info.LastImport = time.UnixMilli(md.TimeStamp).Format("2006-01-02 15:04")
	}
	if len(md.TimeGrid.Days) > 0 {
		for i, u := range md.TimeGrid.Days[0].Units {
			info.Periods = append(info.Periods, Period{cmp.Or(u.Label, strconv.Itoa(i+1)), string(u.StartTime), string(u.EndTime)})
		}
	}
	for _, h := range md.Holidays {
		if sy.Name == "" || (sy.StartDate <= h.StartDate && h.StartDate <= sy.EndDate) {
			info.Holidays = append(info.Holidays, Holiday{h.LongName, string(h.StartDate), string(h.EndDate)})
		}
	}
	slices.SortFunc(info.Holidays, func(a, b Holiday) int { return cmp.Compare(a.Start, b.Start) })
	return info, nil
}
