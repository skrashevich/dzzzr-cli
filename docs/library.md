# Использование как библиотеки

Пакет `github.com/skrashevich/dzzzr-cli/dzzzr` — клиент движка для встраивания в свой код.

## Библиотека

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

func main() {
	c := dzzzr.New("moscow", dzzzr.WithCredentials(dzzzr.Credentials{Captain: "captain", Pin: "1234"}))
	ctx := context.Background()

	login, err := c.Login(ctx, "user", "password")
	if err != nil {
		log.Fatal(err)
	}
	if !login.OK() {
		log.Fatalf("вход не выполнен: %s", dzzzr.LoginCodeText(login.Code.Int()))
	}

	st, err := c.GetGame(ctx)
	if err != nil {
		log.Fatal(err)
	}
	if st.Level != nil {
		fmt.Printf("Уровень %d: найдено %d из %d\n", st.Level.LevelNumber, st.Level.CodesFounded, st.Level.TotalCodes)
		fmt.Println(dzzzr.StripHTML(st.Level.Question))
	}

	res, err := c.SendCode(ctx, "(112)D45R92")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("[%d] %s (принят: %v)\n", res.Err, res.Text, res.Accepted())
}
```

### Опции клиента

| Опция | Описание |
| --- | --- |
| `WithBaseURL(url)` | Другой адрес движка (mock, зеркало) |
| `WithHTTP()` | HTTP вместо HTTPS |
| `WithInsecureTLS()` | Не проверять сертификат |
| `WithTimeout(d)` | Таймаут HTTP-клиента |
| `WithUserAgent(ua)` | User-Agent |
| `WithDebugLogger(f)` | Лог запросов и ответов |
| `WithHARRecording(bool)` | Запись трафика, см. `ExportHAR` |
| `WithCredentials(c)` | Логин капитана и PIN |
| `WithSession(token)` | Готовый токен сессии |
| `WithAdminCredentials(l, p)` | Учётные данные организатора |
| `WithAdminDelay(d)` | Пауза между админскими POST (по умолчанию 500 мс) |
