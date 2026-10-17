# Traccia ürün kararları

## Amaç

Tek kişinin birden fazla ürününü ve ekibini yönetmesi: çalışma alanları, ekipler, projeler, issue'lar, talepler, belgeler ve geliştirme araçları aynı sistemde takip edilir. Ticari SaaS/faturalandırma kapsamda değildir.

## Onaylanan kapsam

- Workspace → ekip → proje düzeni; çalışma alanı tüm veri ve erişim sınırının köküdür.
- Issue yaşam döngüsü, alt işler, ilişkiler, etiketler, yorumlar, dosyalar, geçmiş, listeler, Kanban, cycles, şablonlar ve raporlar.
- Paneldeki tüm ürün işlemlerinin API ve MCP üzerinden kayıpsız kullanılabilmesi. Aynı uygulama servisleri ve yetki kontrolleri; bütün içerik ve sayfalama desteği.
- Plate.js: issue, yorum, belge ve widget için ortak sürümlü JSON içerik modeli. Markdown dışa/içe aktarım biçimidir.
- GitHub App bağlantısı, repo/PR ilişkilendirme, güvenilir webhook işleme, release/deploy takibi.
- Traccia'ya doğrudan bildirim gönderen yerleşik widget; eski Linear API anahtarlarını taşımaz.
- Ürün/ortam kataloğu, ortak talep havuzu, bağlantılı bilgi alanı, otomasyon kuralları ve MCP/ajan işlem geçmişi.
- Türkçe öncelikli i18n, tarayıcı ve mobil web, VPS kurulumu ve yedek/geri yükleme.

Slack, ticari abonelik/billing, yerel mobil uygulama ve ayrı bir yetki matrisi arayüzü bu kapsamın parçası değildir.

## Uygulama kuralları

İşlem sözleşmeleri panel, API ve MCP'den bağımsız tanımlanır. Worker, arama, dosya ve realtime kapsamları workspace sınırını korur. Idempotency, sürüm denetimi ve kalıcı teslimat kayıtları issue ve webhook tekrarlarında veri kaybını ve çoğaltmayı önlemelidir.

Widget teknik veri, ekran kaydı ve oturum tekrarı yetenekleri ayrı ayarlardır; ilk başlangıç bunları toplamaya başlamaz. Widget gönderen kimliği host uygulamasının imzaladığı sunucu bağlamıyla doğrulanmalıdır. Herkese açık bildirim politikasının ayrıntıları widget aşamasında kesinleştirilecektir.

## Başlangıç sınırı

Bu belge ürünün tamamlandığı anlamına gelmez. İlk commit yalnız yeniden kullanılabilir platform başlangıcını, Traccia kimliğini ve geliştirme kurallarını sağlar. Kalıtılan platform rehberleri bugünkü kodu; bu belge hedef ürün kararlarını açıklar.
