package main

import (
	"embed"
	"log"

	"dbird/internal/dbx"
	"dbird/internal/store"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

func main() {
	path, err := store.DefaultPath()
	if err != nil {
		log.Fatal(err)
	}
	st, err := store.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	dbm := dbx.NewManager()
	conns := &ConnectionService{store: st, dbm: dbm}

	app := application.New(application.Options{
		Name:        "dbird",
		Description: "A lightweight SQL client",
		Icon:        appIcon,
		Services: []application.Service{
			application.NewService(conns),
			application.NewService(&QueryService{conns: conns, dbm: dbm}),
			application.NewService(&WorkspaceService{store: st}),
			application.NewService(&FileService{}),
			application.NewService(NewUpdateService()),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "dbird",
		Width:            1280,
		Height:           800,
		MinWidth:         720,
		MinHeight:        480,
		BackgroundColour: application.NewRGB(24, 25, 29),
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
