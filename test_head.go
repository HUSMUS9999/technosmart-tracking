package main

import (
	"fmt"
	"net/http"
)

func main() {
	url := "https://drive.google.com/uc?export=download&id=ks5D6uauMaZA1K8YSfhBkYpypvDSdi1j3Pm8vaCKhmcAI"
	resp, err := http.Head(url)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("Headers for file 1:\n")
	for k, v := range resp.Header {
		fmt.Printf("%s: %v\n", k, v)
	}

	url2 := "https://drive.google.com/uc?export=download&id=xY0QKR-rYDFKzh_upVHxAxAW_okLvMANmk02d-9UtJy3s"
	resp2, err := http.Head(url2)
	if err == nil {
		fmt.Printf("\nHeaders for file 2:\n")
		for k, v := range resp2.Header {
			fmt.Printf("%s: %v\n", k, v)
		}
	}
}
