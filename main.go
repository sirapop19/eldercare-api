package main

import (
	"database/sql"
	"log"
	"os"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"golang.org/x/crypto/bcrypt"
)

var db *sql.DB

func initDB() {
	dsn := "root:@tcp(127.0.0.1:3306)/eldercare_db?charset=utf8mb4&parseTime=True&loc=Local"
	var err error
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("❌ เชื่อมต่อฐานข้อมูลล้มเหลว: %v", err)
	}

	if err = db.Ping(); err != nil {
		log.Fatalf("❌ ไม่สามารถติดต่อฐานข้อมูล MySQL ได้: %v", err)
	}
	log.Println("✅ เชื่อมต่อฐานข้อมูล MySQL สำเร็จ!")
}

type HealthData struct {
	DeviceID  string  `json:"device_id"`
	BPM       int     `json:"bpm"`
	SpO2      int     `json:"spo2"`
	Battery   int     `json:"battery"`
	BP        string  `json:"bp"`
	IsFalling bool    `json:"is_falling"`
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	Timestamp string  `json:"timestamp"`
}

type AlertData struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	HeartRate int    `json:"heart_rate"`
	Timestamp string `json:"timestamp"`
}

var (
	currentData HealthData
	mutex       sync.Mutex
)

func main() {
	initDB() // 🌟 เริ่มต้นเชื่อมต่อ MySQL
	defer db.Close()

	app := fiber.New()

	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "Origin, Content-Type, Accept",
	}))

	// ==========================================
	// 📌 ส่วนที่ 1: ตรวจสอบสถานะเซิร์ฟเวอร์
	// ==========================================
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("ElderCare API is running successfully with MySQL! 🚀")
	})

	// ==========================================
	// 📌 ส่วนที่ 2: API Project (HealthData)
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

		if newData.Timestamp == "" {
			newData.Timestamp = time.Now().Format("2006-01-02 15:04:05")
		}

		mutex.Lock()
		currentData = newData
		mutex.Unlock()

		// บันทึกลงตาราง Health_Data ใน MySQL จริง
		query := "INSERT INTO Health_Data (elderly_id, heart_rate, blood_oxygen, blood_pressure, record_timestamp) VALUES (?, ?, ?, ?, ?)"
		_, err := db.Exec(query, 1, newData.BPM, newData.SpO2, newData.BP, newData.Timestamp)
		if err != nil {
			log.Printf("❌ บันทึก Health Data ลง DB ไม่สำเร็จ: %v", err)
		}

		return c.JSON(fiber.Map{"status": "success", "data": currentData})
	})

	app.Post("/api/sos", func(c *fiber.Ctx) error {
		mutex.Lock()
		currentData.IsFalling = true
		mutex.Unlock()
		return c.JSON(fiber.Map{"status": "sos_triggered"})
	})

	// ==========================================
	// 📌 ส่วนที่ 3: API (History) - ดึงและบันทึกจาก MySQL
	// ==========================================
	app.Post("/api/alert", func(c *fiber.Ctx) error {
		var data AlertData
		if err := c.BodyParser(&data); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "Invalid input"})
		}

		if data.Timestamp == "" {
			data.Timestamp = time.Now().Format("2006-01-02 15:04:05")
		}

		// บันทึกลงตาราง Emergency_Alert ใน MySQL จริง
		query := "INSERT INTO Emergency_Alert (elderly_id, alert_type, title, heart_rate, alert_timestamp) VALUES (?, ?, ?, ?, ?)"
		_, err := db.Exec(query, 1, data.Type, data.Title, data.HeartRate, data.Timestamp)
		if err != nil {
			log.Printf("❌ บันทึก Alert ลง DB ไม่สำเร็จ: %v", err)
		}

		log.Printf("✅ Received & Saved Alert: %+v\n", data)
		return c.SendStatus(200)
	})

	app.Get("/api/history", func(c *fiber.Ctx) error {
		rows, err := db.Query("SELECT alert_type, title, heart_rate, alert_timestamp FROM Emergency_Alert ORDER BY alert_id DESC LIMIT 50")
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Database error"})
		}
		defer rows.Close()

		var historyList []AlertData
		for rows.Next() {
			var item AlertData
			if err := rows.Scan(&item.Type, &item.Title, &item.HeartRate, &item.Timestamp); err != nil {
				continue
			}
			historyList = append(historyList, item)
		}

		if historyList == nil {
			return c.JSON([]AlertData{})
		}
		return c.JSON(historyList)
	})

	app.Get("/api/history/:type", func(c *fiber.Ctx) error {
		alertType := c.Params("type")
		rows, err := db.Query("SELECT alert_type, title, heart_rate, alert_timestamp FROM Emergency_Alert WHERE alert_type = ? ORDER BY alert_id DESC LIMIT 50", alertType)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Database error"})
		}
		defer rows.Close()

		var filtered []AlertData
		for rows.Next() {
			var item AlertData
			if err := rows.Scan(&item.Type, &item.Title, &item.HeartRate, &item.Timestamp); err != nil {
				continue
			}
			filtered = append(filtered, item)
		}

		if filtered == nil {
			return c.JSON([]AlertData{})
		}
		return c.JSON(filtered)
	})

	// ==========================================
	// 📌 ส่วนที่ 4: ตั้งค่า Port สำหรับ Render.com
	// ==========================================
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server is starting on port %s...", port)
	log.Fatal(app.Listen("0.0.0.0:" + port))
}

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	return string(bytes), err
}
