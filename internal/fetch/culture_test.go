package fetch

import (
	"strings"
	"testing"
)

// Made-up charts in the data sets' shapes.

func TestParseHot100(t *testing.T) {
	body := `{"date":"2026-10-31","data":[
		{"song":"Druga","artist":"B","this_week":2,"last_week":1,"peak_position":1,"weeks_on_chart":9},
		{"song":"Prva","artist":"A","this_week":1,"last_week":null,"peak_position":1,"weeks_on_chart":1},
		{"song":"Jedanaesta","artist":"C","this_week":11,"last_week":12,"peak_position":5,"weeks_on_chart":3}]}`
	week, top, err := ParseHot100([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if week.Format("2006-01-02") != "2026-10-31" || len(top) != 2 || top[0].Title != "Prva" || top[0].Last != 0 || top[1].Last != 1 {
		t.Errorf("%s %+v", week, top)
	}
	if _, _, err := ParseHot100([]byte(`<html>`)); err == nil {
		t.Error("a page that is not the data set")
	}
}

func TestParseBillboard200(t *testing.T) {
	body := "chart_week,current_week,title,performer,last_week,peak_pos,wks_on_chart,wks_at_no1\n" +
		"2026-10-24,1,Stari album,Stari bend,1,1,5,2\n" +
		"2026-10-31,2,\"Album, drugi\",Bend,1,1,6,NA\n" +
		"2026-10-31,1,Novi album,Novi bend,NA,1,1,1\n" +
		"2026-10-31,11,Izvan,Netko,10,3,8,NA\n"
	week, top, err := ParseBillboard200(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if week.Format("2006-01-02") != "2026-10-31" || len(top) != 2 || top[0].Title != "Novi album" || top[0].Last != 0 ||
		top[1].Title != "Album, drugi" || top[1].Weeks != 6 {
		t.Errorf("%s %+v", week, top)
	}
	if _, _, err := ParseBillboard200(strings.NewReader("a,b\n1,2\n")); err == nil {
		t.Error("a CSV without the columns")
	}
}
