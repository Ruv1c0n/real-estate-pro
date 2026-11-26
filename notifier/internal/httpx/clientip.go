// Package httpx — небольшие HTTP-утилиты, общие для обработчиков notifier.
package httpx

import (
	"net"
	"net/http"
)

// ClientIP достаёт IP клиента из RemoteAddr. X-Forwarded-For не читается
// сознательно: заголовок подделывается любым клиентом, а доверенного
// прокси перед сервисом пока нет. Когда появится TLS-прокси (nginx),
// здесь надо будет брать адрес из XFF только при запросе от доверенного
// источника — иначе лимитер начнёт лимитировать прокси, а не клиентов.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// Не host:port (например, тестовый RemoteAddr без порта) —
		// используем как есть, это единственная доступная идентичность.
		return r.RemoteAddr
	}
	return host
}
