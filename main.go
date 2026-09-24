package main

import (
	"database/sql"
	"log"
	"os"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/golang-jwt/jwt/v5"
	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

var db *sql.DB

func initDB() {
	dsn := "postgresql://postgres.zebevhrnhhrlpfsiqysf:ElderCare2026DB@aws-0-ap-northeast-1.pooler.supabase.com:5432/postgres"
	var err error
	db, err = sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("❌ เชื่อมต่อฐานข้อมูลล้มเหลว: %v", err)
	}

	if err = db.Ping(); err != nil {
		log.Fatalf("❌ ไม่สามารถติดต่อฐานข้อมูล PostgreSQL ได้: %v", err)
	}
	log.Println("✅ เชื่อมต่อฐานข้อมูล PostgreSQL สำเร็จ!")
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

type Medicine struct {
	ID       int    `json:"id"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	IsTaken  bool   `json:"is_taken"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

type SocialLoginRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
}

type ForgotPasswordRequest struct {
	Email       string `json:"email"`
	NewPassword string `json:"new_password"`
}

type Claims struct {
	Email string `json:"email"`
	jwt.RegisteredClaims
}

var jwtKey = []byte("YOUR_SUPER_SECRET_KEY_ELDERCARE")

var (
	currentData HealthData
	mutex       sync.Mutex
)

func main() {
	initDB()
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
		return c.SendString("ElderCare API is running successfully with PostgreSQL & Supabase! 🚀")
	})

	// ==========================================
	// 📌 ส่วนที่ 2: ระบบสมาชิก & ยืนยันตัวตน (Auth API)
	// ==========================================
	app.Post("/api/login", func(c *fiber.Ctx) error {
		var req LoginRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"message": "Invalid input"})
		}

		var storedPassword string
		var userName string
		query := "SELECT name, password FROM users WHERE email = $1"
		err := db.QueryRow(query, req.Email).Scan(&userName, &storedPassword)
		if err != nil {
			return c.Status(401).JSON(fiber.Map{"message": "ไม่พบอีเมลนี้ในระบบ"})
		}

		err = bcrypt.CompareHashAndPassword([]byte(storedPassword), []byte(req.Password))
		if err != nil {
			return c.Status(401).JSON(fiber.Map{"message": "รหัสผ่านไม่ถูกต้อง"})
		}

		expirationTime := time.Now().Add(24 * time.Hour)
		claims := &Claims{
			Email: req.Email,
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(expirationTime),
			},
		}

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString(jwtKey)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"message": "Could not generate token"})
		}

		return c.JSON(fiber.Map{
			"message": "เข้าสู่ระบบสำเร็จ",
			"token":   tokenString,
			"name":    userName,
		})
	})

	app.Post("/api/register", func(c *fiber.Ctx) error {
		var req RegisterRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"message": "Invalid input"})
		}

		var exists bool
		err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE email = $1)", req.Email).Scan(&exists)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"message": "Database error"})
		}

		if exists {
			return c.Status(409).JSON(fiber.Map{"message": "อีเมลนี้ถูกใช้งานแล้ว"})
		}

		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"message": "Server error"})
		}

		query := "INSERT INTO users (email, password, name) VALUES ($1, $2, $3)"
		_, err = db.Exec(query, req.Email, string(hashedPassword), req.Name)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"message": "Failed to register"})
		}

		return c.JSON(fiber.Map{"message": "สมัครสมาชิกสำเร็จ"})
	})

	app.Post("/api/social-login", func(c *fiber.Ctx) error {
		var req SocialLoginRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"message": "Invalid input"})
		}

		var userID int
		var userName string
		queryCheck := "SELECT id, name FROM users WHERE email = $1"
		err := db.QueryRow(queryCheck, req.Email).Scan(&userID, &userName)

		if err != nil {
			insertQuery := "INSERT INTO users (email, password, name) VALUES ($1, $2, $3) RETURNING id"
			err = db.QueryRow(insertQuery, req.Email, "SOCIAL_LOGIN_"+req.Provider, req.Name).Scan(&userID)
			if err != nil {
				return c.Status(500).JSON(fiber.Map{"message": "Database error"})
			}
			userName = req.Name
		}

		return c.JSON(fiber.Map{
			"message": "เข้าสู่ระบบด้วย " + req.Provider + " สำเร็จ",
			"name":    userName,
			"user_id": userID,
		})
	})

	app.Post("/api/forgot-password", func(c *fiber.Ctx) error {
		var req ForgotPasswordRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"message": "Invalid input"})
		}

		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"message": "Server error"})
		}

		query := "UPDATE users SET password = $1 WHERE email = $2"
		result, err := db.Exec(query, string(hashedPassword), req.Email)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"message": "Database error"})
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			return c.Status(404).JSON(fiber.Map{"message": "ไม่พบอีเมลนี้ในระบบ"})
		}

		return c.JSON(fiber.Map{"message": "เปลี่ยนรหัสผ่านสำเร็จ"})
	})
	// ==========================================
	// 📌 ส่วนที่ 2.1: อัปเดตโปรไฟล์ผู้ดูแล
	// ==========================================
	app.Put("/api/caregiver/:id", func(c *fiber.Ctx) error {
		id := c.Params("id")
		var data struct {
			Name  string `json:"name"`
			Email string `json:"email"`
			Phone string `json:"phone"`
		}
		if err := c.BodyParser(&data); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "Invalid input"})
		}

		query := "UPDATE caregivers SET name = $1, email = $2, phone = $3 WHERE id = $4"
		_, err := db.Exec(query, data.Name, data.Email, data.Phone, id)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Database error"})
		}

		return c.JSON(fiber.Map{"message": "อัปเดตโปรไฟล์ผู้ดูแลสำเร็จ"})
	})

	// ==========================================
	// 📌 ส่วนที่ 3: ข้อมูลสุขภาพ & SOS (HealthData)
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

		query := "INSERT INTO health_data (elderly_id, heart_rate, blood_oxygen, blood_pressure, record_timestamp) VALUES ($1, $2, $3, $4, $5)"
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
	// 📌 ส่วนที่ 4: ประวัติการแจ้งเตือน (Alert & History)
	// ==========================================
	app.Post("/api/alert", func(c *fiber.Ctx) error {
		var data AlertData
		if err := c.BodyParser(&data); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "Invalid input"})
		}

		if data.Timestamp == "" {
			data.Timestamp = time.Now().Format("2006-01-02 15:04:05")
		}

		query := "INSERT INTO emergency_alert (elderly_id, alert_type, title, heart_rate, alert_timestamp) VALUES ($1, $2, $3, $4, $5)"
		_, err := db.Exec(query, 1, data.Type, data.Title, data.HeartRate, data.Timestamp)
		if err != nil {
			log.Printf("❌ บันทึก Alert ลง DB ไม่สำเร็จ: %v", err)
		}

		return c.SendStatus(200)
	})

	app.Get("/api/history", func(c *fiber.Ctx) error {
		rows, err := db.Query("SELECT alert_type, title, heart_rate, alert_timestamp FROM emergency_alert ORDER BY alert_id DESC LIMIT 50")
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

	// ==========================================
	// 📌 ส่วนที่ 5: จัดการรายการยา (Medicine API)
	// ==========================================
	app.Get("/api/medicines/:elderly_id", func(c *fiber.Ctx) error {
		elderlyID := c.Params("elderly_id")
		rows, err := db.Query("SELECT id, title, subtitle, is_taken FROM medicine WHERE elderly_id = $1", elderlyID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Database error"})
		}
		defer rows.Close()

		var meds []Medicine
		for rows.Next() {
			var m Medicine
			if err := rows.Scan(&m.ID, &m.Title, &m.Subtitle, &m.IsTaken); err != nil {
				continue
			}
			meds = append(meds, m)
		}

		if meds == nil {
			return c.JSON([]Medicine{})
		}
		return c.JSON(meds)
	})

	app.Post("/api/medicines", func(c *fiber.Ctx) error {
		var m Medicine
		if err := c.BodyParser(&m); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "Invalid input"})
		}

		query := "INSERT INTO medicine (elderly_id, title, subtitle, is_taken) VALUES ($1, $2, $3, $4)"
		_, err := db.Exec(query, 1, m.Title, m.Subtitle, m.IsTaken)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Failed to save medicine"})
		}

		return c.JSON(fiber.Map{"status": "success", "message": "เพิ่มรายการยาสำเร็จ"})
	})

	app.Delete("/api/medicines/:id", func(c *fiber.Ctx) error {
		id := c.Params("id")
		query := "DELETE FROM medicine WHERE id = $1"
		_, err := db.Exec(query, id)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Failed to delete medicine"})
		}

		return c.JSON(fiber.Map{"status": "success", "message": "ลบรายการยาสำเร็จ"})
	})

	// ==========================================
	// 📌 ส่วนที่ 6: รันเซิร์ฟเวอร์ (Render Port)
	// ==========================================
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server is starting on port %s...", port)
	log.Fatal(app.Listen("0.0.0.0:" + port))
}
