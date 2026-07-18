# Pinterest-only подбор обложек

**Дата:** 2026-07-19
**Статус:** одобрено к реализации

## Проблема

Подбор обложек (`covers.Search`) работает через Bing Images и часто отдаёт
нерелевантные картинки. Пользователь хочет подбор именно через Pinterest.

Раньше внутренний API Pinterest отдавал анонимам `403`, поэтому основным
источником был Bing. Проверка от 2026-07-19 показала: API `403` **только без
бутстрапа сессии**. Если сначала сделать `GET https://www.pinterest.com/`,
получить cookie `csrftoken` (+ `_pinterest_sess`, `_auth`, `_routing_id`) и
слать запрос поиска с этим cookie-jar и заголовком `X-CSRFToken`, API отдаёт
`200` с настоящими пинами (проверено: 25 пинов с `i.pinimg.com/originals/...`
и `bookmark` для пагинации).

## Решение

Источник обложек — **только Pinterest**. Bing и og:image-скрейп удаляются.
При неудаче поиска — пустой результат («ничего не найдено»), без фолбэка.

Все изменения — в `backend/internal/infrastructure/covers/covers.go`, плюс
конструктор клиента в `backend/internal/usecase/covers.go`. UI не меняется:
эндпоинт `/api/covers/search` и фронтовый `coversSearch` остаются как есть.

### 1. Cookie-jar на клиенте

`NewCoverService` создаёт `http.Client` с `cookiejar.New(nil)`. Jar служит
кэшем сессии между бутстрап-GET и запросами поиска. Сигнатура
`covers.Search(ctx, httpc, query, limit)` не меняется — jar живёт на
переданном клиенте.

### 2. Ленивый бутстрап сессии

Перед поиском `Search` проверяет, есть ли в jar для `https://www.pinterest.com/`
cookie `csrftoken`. Если нет — делает `GET https://www.pinterest.com/` с
browser-UA, чтобы jar наполнился cookies. Если после GET `csrftoken` так и не
появился — возвращаем ошибку (искать бессмысленно).

### 3. Поиск (`pinterestSearch`)

Запрос:

```
GET https://www.pinterest.com/resource/BaseSearchResource/get/
    ?source_url=%2Fsearch%2Fpins%2F%3Fq%3D<query>
    &data=<url-encoded JSON>
```

`data` (JSON):
```json
{"options":{"query":"<q>","scope":"pins","page_size":25,"bookmarks":["<bm>"]},"context":{}}
```
На первой странице `bookmarks` опускаем (или пустой).

Заголовки:
- `User-Agent: <browserUA>`
- `Accept: application/json, text/javascript, */*, q=0.01`
- `X-Requested-With: XMLHttpRequest`
- `X-CSRFToken: <значение cookie csrftoken из jar>`
- `Referer: https://www.pinterest.com/search/pins/?q=<query>`
- `X-APP-VERSION` и `X-Pinterest-PWS-Handler` — фиксированные строки
  (точные значения уточняются при реализации; при `200` без них — не слать).

Парсинг ответа:
- `resource_response.data.results[]` → для каждого пина:
  - `images.orig.url` → `Image.Full` (+ `Width`/`Height` из `orig`)
  - `images["236x"].url` → `Image.Thumb` (фолбэк на `orig.url`, если нет)
  - `id` → `Image.ID`
  - пины без `orig.url` пропускаем.
- `resource_response.bookmark` → строка для следующей страницы.

### 4. Пагинация

`Search` крутит цикл: запрашивает страницу, добавляет пины к результату, берёт
`bookmark` для следующего запроса. Останавливается, когда набрано `limit`
пинов, либо `bookmark` пустой/повторяется, либо контекст отменён. Итог
обрезается до `limit`. Дедуп по `Full` URL.

Ограничение защиты: не более, скажем, 12 страниц за один `Search`, чтобы не
зациклиться при странном ответе.

### 5. Восстановление сессии

Если запрос страницы вернул `403` (cookie протухли) — один раз чистим сессию,
делаем бутстрап заново и повторяем текущий запрос. Повторный `403` → ошибка.

### 6. Удаляемый код

Из `covers.go` убираются: `bingMerged`, `bingImages`, `searchVariants`,
`variantDelay`, `capped`, `isPin`, `variantQuery`, `mAttrRe`, `pinterestOG`,
`pinImgRe`. `Download` и тип `Image` остаются без изменений.

### 7. Обработка ошибок

- Транспортные/парсинг-ошибки и неудачный бутстрап → `error`. Usecase вернёт
  ошибку, HTTP-хендлер → фронт покажет «ничего не найдено» (текущее поведение).
- Пустой список пинов при успешном ответе → пустой срез + `nil`.

## Тестирование

`covers_test.go` с `httptest.Server`, роутящим по пути:
- `/` → `Set-Cookie: csrftoken=<t>` (+ 200), имитация бутстрапа.
- `/resource/BaseSearchResource/get/` → отдаёт зафиксированный JSON-фикстур
  с `results` и `bookmark`.

Клиент теста — с `cookiejar`, `httpc` указывает на тестовый сервер (через
подмену базового URL — вынести базу Pinterest в переменную пакета, чтобы тест
её переопределял).

Проверяем:
1. Маппинг `results → []Image` (Full/Thumb/ID/Width/Height, пропуск без orig).
2. Заголовок `X-CSRFToken` в запросе поиска равен выданному cookie.
3. Пагинация: два фикстура-страницы с разными `bookmark` склеиваются, `limit`
   обрезает.
4. `403` на первой попытке → ре-бутстрап → успех на второй.
5. Дедуп одинаковых `Full` между страницами.

## Вне области

- UI-переключатель источников (решено: только Pinterest).
- Кэширование результатов поиска между запросами.
- Аутентификация под аккаунтом Pinterest (работаем анонимно).
