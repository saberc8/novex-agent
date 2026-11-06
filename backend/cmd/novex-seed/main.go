package main

import (
	"context"
	"log"
	"voc-go-backend/internal/infrastructure/db"
	"voc-go-backend/internal/novex"
)

func main() {
	pg, e := db.NewPostgres(db.LoadConfigFromEnv())
	if e != nil {
		log.Fatal(e)
	}
	defer pg.Close()
	if e = db.AutoMigrate(pg); e != nil {
		log.Fatal(e)
	}
	s, e := novex.New(pg, nil)
	if e != nil {
		log.Fatal(e)
	}
	if e = s.Seed(context.Background()); e != nil {
		log.Fatal(e)
	}
	log.Print("Novex demo seed completed: users a/b/c/d/e/x + admin, 2 external sources, 6 resources, scoped grants and private Markdown.")
}
