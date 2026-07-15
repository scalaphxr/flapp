package youtube

// Встроенные OAuth-креды приложения: с ними канал подключается в один клик,
// без похода в Google Cloud Console. Значения подставляются файлом
// defaults_local.go (генерируется локально, в git не попадает) либо при сборке:
//
//	go build -ldflags "-X github.com/flapp/core/internal/infrastructure/youtube.DefaultClientID=... \
//	                   -X github.com/flapp/core/internal/infrastructure/youtube.DefaultClientSecret=..."
//
// Секрет клиента типа «Desktop app» по модели Google не считается
// конфиденциальным (PKCE), поэтому его допустимо вшивать в бинарник.
// Креды из настроек пользователя всегда имеют приоритет над этими.
var (
	DefaultClientID     string
	DefaultClientSecret string
)
