package middleware

import (
    "compress/gzip"
    "strings"

    "github.com/gin-gonic/gin"
)

// gzipWriter wraps gin.ResponseWriter to write gzipped data.
type gzipWriter struct {
    gin.ResponseWriter
    gz *gzip.Writer
}

func (w *gzipWriter) Write(b []byte) (int, error) {
    return w.gz.Write(b)
}

// Gzip enables gzip compression when the client accepts it.
func Gzip() gin.HandlerFunc {
    return func(c *gin.Context) {
        ae := c.GetHeader("Accept-Encoding")
        if !strings.Contains(ae, "gzip") {
            c.Next()
            return
        }

        // Set headers for gzip response
        c.Header("Content-Encoding", "gzip")
        c.Header("Vary", "Accept-Encoding")
        // Content-Length will be unknown after compression
        c.Writer.Header().Del("Content-Length")

        gz, _ := gzip.NewWriterLevel(c.Writer, gzip.BestSpeed)
        defer gz.Close()

        gw := &gzipWriter{ResponseWriter: c.Writer, gz: gz}
        c.Writer = gw

        c.Next()
    }
}

