package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	wbc "github.com/peterouob/pionWebRTC/pkg/webrtc"
	"github.com/peterouob/pionWebRTC/pkg/websocket"
)

func main() {
	if err := wbc.InitManager(); err != nil {
		log.Println("webRTC init error")
	}

	r := gin.Default()
	r.Use(Cors)
	r.GET("/ws", func(c *gin.Context) {
		websocket.HandleWebsocket(c.Writer, c.Request)
	})

	r.GET("/", func(c *gin.Context) {
		c.File("index.html")
	})

	log.Println("Start server ...")
	if err := r.Run(":8080"); err != nil {
		log.Fatalln("Server failed to start:", err)
	}
}

func Cors(c *gin.Context) {
	method := c.Request.Method
	c.Header("Access-Control-Allow-Origin", c.GetHeader("Origin"))
	c.Header("Access-Control-Allow-Methods", "POST, GET, PUT, DELETE, OPTIONS")
	c.Header("Access-Control-Allow-Headers", "Content-Type, Content-Length, Token")
	c.Header("Access-Control-Expose-Headers", "Access-Control-Allow-Headers, Token")
	c.Header("Access-Control-Allow-Credentials", "true")
	if method == "OPTIONS" {
		c.AbortWithStatus(http.StatusNoContent)
		return
	}
	c.Next()
}
