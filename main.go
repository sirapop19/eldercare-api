package main

import (
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
)

type HealthData struct {
	DeviceID  string  `json:"device_id"`
	BPM       int     `json:"bpm"`
	SpO2      int     `json:"spo2"`
	Battery   int     `json:"battery"`
	BP        string  `json:"bp"`
	IsFalling bool    `json:"is_falling"`
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
}

var currentData HealthData

func main() {
	app := fiber.New()

	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("ElderCare API is running successfully! 🚀")
	})

	app.Get("/api/health", func(c *fiber.Ctx) error {
		return c.JSON(currentData)
	})

	app.Post("/api/health", func(c *fiber.Ctx) error {
		var newData HealthData
		if err := c.BodyParser(&newData); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "Invalid input"})
		}

		currentData = newData
		return c.JSON(fiber.Map{"status": "success"})
	})

	app.Post("/api/sos", func(c *fiber.Ctx) error {
		currentData.IsFalling = true
		return c.JSON(fiber.Map{"status": "sos_triggered"})
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server is starting on port %s", port)
	log.Fatal(app.Listen(":" + port))
}