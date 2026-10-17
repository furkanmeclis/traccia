# Traccia

Projeler, talepler, teknik bilgi ve geliştirme araçları için kişisel çalışma merkezi. Türkçe öncelikli, tarayıcı ve mobil web odaklı; VPS üzerinde çalışacak şekilde geliştiriliyor.

## Durum

Bu ilk sürüm, sahibinin Next.js/Go boilerplate kodundan hazırlanmış **geliştirme başlangıcıdır**. Mevcut kimlik doğrulama, platform paneli, organizasyon temelleri, dosya depolama, arama, bildirimler ve iş kuyruğu altyapısı korunmuştur. Traccia issue/proje akışları, tam API/MCP erişimi, GitHub PR/deploy takibi ve yerleşik widget henüz uygulanmamıştır.

Yeni ürün editörü **Plate.js** olacaktır. Boilerplate ile gelen Lexical bileşenleri yalnız mevcut platform ekranlarının çalışması için korunmuştur; Plate dönüşümü tamamlanmış değildir.

## Altyapı

- Next.js + React + TypeScript; aynı kaynaktan Go API'ye erişen BFF.
- Go `net/http`, PostgreSQL, pgx/sqlc.
- Redis/Asynq, Centrifugo, Meilisearch, S3/MinIO.
- Mevcut i18n altyapısı; ürünün varsayılan dili Türkçe.

## Yerel başlangıç

Gereksinimler: Docker Compose, Go (`backend/go.mod`), Node (`frontend/.nvmrc`), pnpm ve backend geliştirme araçları. Ayrıntılar: [geliştirme rehberi](docs/DEVELOPMENT.md).

```sh
cp .env.example .env
# Yerel gizli değerleri ve aynı makinedeki diğer uygulamalarla çakışabilecek portları düzenle.
make local-dev
```

İlk yönetici için ayrı terminalde `make create-super-admin` çalıştırılır. Platform giriş adresi `http://localhost:3000/platform/login` olur. Kopyalanan örnek değerler üretim için kullanılmamalıdır. Gerçek `.env` dosyaları Git'e eklenmez.

## Geliştirme sırası

1. Ortak işlem sözleşmeleri, workspace sınırları ve API/MCP kapsam doğrulaması.
2. Ekipler, projeler ve issue yaşam döngüsü.
3. Plate editörü, belgeler, responsive panel ve MCP.
4. GitHub bağlantısı, yerleşik widget, ürün/ortam kataloğu ve talep havuzu.
5. Otomasyonlar, release/deploy takibi ve ajan işlem geçmişi.

Onaylanan kapsam: [ürün kararları](docs/PRODUCT.md). Onaylanan 53 işin ayrıntıları [yol haritasında](docs/plan/roadmap.md), makine tarafından okunabilir kayıtları [iş listesinde](docs/plan/issues.json) bulunur. Araştırılan özel depoların kaynak kodu ve yerel inceleme raporları bu depoya eklenmez.

## Doğrulama

```sh
cd backend
go build ./...
go vet ./...
go test ./...
```

```sh
cd frontend
pnpm install --frozen-lockfile
pnpm typecheck
pnpm lint
pnpm i18n:check
```

CI yalnız kontrolleri çalıştırır; otomatik merge veya deploy yapmaz. Veritabanı izolasyonu ve ürün uçtan uca testleri ilgili özellikler geliştirilirken eklenir.

## Kaynak ve lisans

Başlangıç altyapısı, aynı hesap sahibinin `nextjs-go-boilerplate` deposunun `03bf65def2a533eb71dcb7ac89919dfe3f4c626b` sürümünden türetilmiştir. Depoya henüz bir açık kaynak lisansı seçilmemiştir. Üçüncü taraf paketler kendi lisanslarına tabidir.
