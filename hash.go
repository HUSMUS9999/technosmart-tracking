package main
import "golang.org/x/crypto/bcrypt"
import "fmt"
func main() { h, _ := bcrypt.GenerateFromPassword([]byte("password123"), 10); fmt.Println(string(h)) }
