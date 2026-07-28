package iamcli

import (
	"fmt"
	"io"
	"strings"

	"github.com/boombuler/barcode/qr"
)

const qrQuietZoneModules = 4

func PrintQRCode(writer io.Writer, content string) error {
	if writer == nil || strings.TrimSpace(content) == "" {
		return fmt.Errorf("二维码内容不能为空")
	}
	code, err := qr.Encode(content, qr.M, qr.Auto)
	if err != nil {
		return fmt.Errorf("生成 TOTP 二维码: %w", err)
	}
	bounds := code.Bounds()
	for y := -qrQuietZoneModules; y < bounds.Dy()+qrQuietZoneModules; y++ {
		currentBackground := -1
		for x := -qrQuietZoneModules; x < bounds.Dx()+qrQuietZoneModules; x++ {
			background := 47
			if x >= 0 && x < bounds.Dx() && y >= 0 && y < bounds.Dy() {
				red, green, blue, _ := code.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
				if red+green+blue < 3*0xffff/2 {
					background = 40
				}
			}
			if background != currentBackground {
				fmt.Fprintf(writer, "\x1b[%dm", background)
				currentBackground = background
			}
			fmt.Fprint(writer, "  ")
		}
		fmt.Fprintln(writer, "\x1b[0m")
	}
	return nil
}
