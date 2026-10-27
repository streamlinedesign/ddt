package main

import (
	"context"
	"io/fs"

	"tisak/internal/media"
	"tisak/internal/store"
)

// The first categories and sources, so a fresh install prints something real on day one.
// After this the dashboard owns them; Seed never runs again once any category exists.
type seedSource struct {
	name, kind, url, logo string
	per                   int
	zone                  string // for feeds that write local time as GMT
}

var seed = []struct {
	cat     store.Category
	flag    string
	sources []seedSource
}{
	{store.Category{Slug: "vijesti", Title: "Vijesti", Layout: "lead2row3", LeadPicture: true}, "", []seedSource{
		{"HRT Hrvatska", "rss", "https://feed.hrt.hr/vijesti/hrvatska.xml", "hrt.svg", 2, ""},
		{"HRT Svijet", "rss", "https://feed.hrt.hr/vijesti/svijet.xml", "hrt.svg", 2, ""},
		{"HRT EU", "rss", "https://feed.hrt.hr/vijesti/eu.xml", "hrt.svg", 2, ""},
	}},
	{store.Category{Slug: "glazba", Title: "Glazba", Layout: "rows3"}, "", []seedSource{
		{"Loudwire", "rss", "https://loudwire.com/feed/", "loudwire.webp", 2, ""},
		{"Glazba.hr", "rss", "https://glazba.hr/feed/", "", 2, ""},
		{"Muzika.hr", "rss", "https://www.muzika.hr/feed/", "", 2, ""},
	}},
	{store.Category{Slug: "kultura", Title: "Kultura", Layout: "row3"}, "", []seedSource{
		{"HRT Kultura", "rss", "https://feed.hrt.hr/magazin/kultura.xml", "hrt.svg", 2, ""},
		{"Kinofilm", "rss", "https://kinofilm.hr/feed/", "", 1, ""},
	}},
	{store.Category{Slug: "tehnologija", Title: "Tehnologija", Layout: "lead2", LeadPicture: true}, "", []seedSource{
		{"Bug", "rss", "https://www.bug.hr/rss", "bug.svg", 2, ""},
		{"HRT Znanost i tehnologija", "rss", "https://feed.hrt.hr/magazin/znanost-i-tehnologija.xml", "hrt.svg", 1, ""},
	}},
	{store.Category{Slug: "hacker-news", Title: "Hacker News", Layout: "six"}, "y18.svg", []seedSource{
		{"Hacker News", "hn", "https://hnrss.org/frontpage", "y18.svg", 12, ""},
	}},
	{store.Category{Slug: "sport", Title: "Sport", Layout: "rows3"}, "", []seedSource{
		{"Germanijak", "rss", "https://www.germanijak.hr/rss/0.xml", "", 2, "Europe/Zagreb"},
		{"HRT Hrvatski nogomet", "rss", "https://feed.hrt.hr/sport/hrvatski-nogomet.xml", "hrt.svg", 1, ""},
		{"HRT Međunarodni nogomet", "rss", "https://feed.hrt.hr/sport/medunarodni-nogomet.xml", "hrt.svg", 1, ""},
		{"HRT Košarka", "rss", "https://feed.hrt.hr/sport/kosarka.xml", "hrt.svg", 1, ""},
		{"HRT Rukomet", "rss", "https://feed.hrt.hr/sport/rukomet.xml", "hrt.svg", 1, ""},
		{"HRT Ostali sportovi", "rss", "https://feed.hrt.hr/sport/ostali-sportovi.xml", "hrt.svg", 1, ""},
	}},
}

// Seed adds the first categories when there are none. It gives how many it added.
func Seed(ctx context.Context, db *store.Store, pics *media.Store, webFS fs.FS) (int, error) {
	cats, err := db.Categories(ctx)
	if err != nil || len(cats) > 0 {
		return 0, err
	}
	logos := map[string]string{}
	logo := func(file string) (string, error) {
		if file == "" {
			return "", nil
		}
		if h, ok := logos[file]; ok {
			return h, nil
		}
		body, err := fs.ReadFile(webFS, "static/sources/"+file)
		if err != nil {
			return "", err
		}
		h, err := pics.SaveBytes(ctx, body, "logo:"+file, 240)
		logos[file] = h
		return h, err
	}
	for i, s := range seed {
		c := s.cat
		c.Position, c.Enabled, c.InMenu = (i+1)*10, true, c.Layout != "six"
		if c.Flag, err = logo(s.flag); err != nil {
			return i, err
		}
		id, err := db.AddCategory(ctx, c)
		if err != nil {
			return i, err
		}
		for j, src := range s.sources {
			h, err := logo(src.logo)
			if err != nil {
				return i, err
			}
			settings := map[string]string{"url": src.url}
			if src.zone != "" {
				settings["zone"] = src.zone
			}
			_, err = db.AddSource(ctx, store.Source{CategoryID: id, Name: src.name, Kind: src.kind, Logo: h,
				Settings: settings, PerEdition: src.per, Position: (j + 1) * 10, Enabled: true})
			if err != nil {
				return i, err
			}
		}
	}
	return len(seed), nil
}
