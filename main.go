package main

import (
	"log"
	"os"
	"sync"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
)

// 1. โครงสร้างข้อมูลตามเอกสารใน Project ของคุณ
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

// 2. โครงสร้างสำหรับประวัติการแจ้งเตือน (ที่แอปมือถือและนาฬิกาใช้อยู่ปัจจุบัน)
type AlertData struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	HeartRate int    `json:"heart_rate"`
	Timestamp string `json:"timestamp"`
}

var (
	currentData HealthData
	history     []AlertData
	mutex       sync.Mutex // ตัวช่วยป้องกันเซิร์ฟเวอร์ล่มเวลาข้อมูลเข้าพร้อมกัน
)

func main() {
	app := fiber.New()

	// 🌟 สำคัญมาก: เปิดใช้งาน CORS เพื่อให้แอปมือถือสามารถดึงข้อมูลข้ามโดเมนได้ (กัน Error 104)
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "Origin, Content-Type, Accept",
	}))

	// ==========================================
	// 📌 ส่วนที่ 1: ตรวจสอบสถานะเซิร์ฟเวอร์
	// ==========================================
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("ElderCare API is running successfully! 🚀")
	})

	// ==========================================
	// 📌 ส่วนที่ 2: API ตามเอกสาร Project (HealthData)
	// ==========================================
	app.Get("/api/health", func(c *fiber.Ctx) error {
		mutex.Lock()
		defer mutex.Unlock()
		return c.JSON(currentData)
	})

	app.Post("/api/health", func(c *fiber.Ctx) error {
		var newData HealthData
		if err := c.BodyParser(&newData); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "Invalid input"})
		}

		mutex.Lock()
		currentData = newData
		mutex.Unlock()
		return c.JSON(fiber.Map{"status": "success", "data": currentData})
	})

	app.Post("/api/sos", func(c *fiber.Ctx) error {
		mutex.Lock()
		currentData.IsFalling = true
		mutex.Unlock()
		return c.JSON(fiber.Map{"status": "sos_triggered"})
	})

	// ==========================================
	// 📌 ส่วนที่ 3: API สำหรับแอปนาฬิกา & มือถือปัจจุบัน (History)
	// (ป้องกันไม่ให้แอปที่เขียนไปก่อนหน้านี้ Error Cannot GET)
	// ==========================================
	app.Post("/api/alert", func(c *fiber.Ctx) error {
		var data AlertData
		if err := c.BodyParser(&data); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "Invalid input"})
		}

		mutex.Lock()
		// นำข้อมูลใหม่ไปต่อไว้หน้าสุดของ List
		history = append([]AlertData{data}, history...)

		// ป้องกัน Memory เซิร์ฟเวอร์เต็ม (เก็บประวัติแค่ 50 รายการล่าสุด)
		if len(history) > 50 {
			history = history[:50]
		}
		mutex.Unlock()

		log.Printf("✅ Received Alert: %+v\n", data)
		return c.SendStatus(200)
	})

	app.Get("/api/history", func(c *fiber.Ctx) error {
		mutex.Lock()
		defer mutex.Unlock()

		// ถ้ายังไม่มีประวัติ ให้ส่ง Array เปล่า [] กลับไป (ป้องกันแอปมือถือพัง)
		if history == nil {
			history = []AlertData{}
		}
		return c.JSON(history)
	})

	// ==========================================
	// 📌 ส่วนที่ 4: ตั้งค่า Port สำหรับ Render.com
	// ==========================================
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server is starting on port %s...", port)
	// สั่งเริ่มทำงานเซิร์ฟเวอร์
	log.Fatal(app.Listen("0.0.0.0:" + port))
}
