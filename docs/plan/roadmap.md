# Traccia roadmap

## Primary goal

A personal workspace for projects, issues, requests, product environments, technical knowledge and releases, operated through Turkish-first web/mobile, API or MCP with equivalent capabilities.

Approved scope: multiple isolated workspaces with teams/projects; issue lifecycle and views; GitHub PR/CI and deploy tracking; native embedded reporting widget; shared Plate.js editor; product/environment catalog; unified intake; linked documents/decisions; rules automation; MCP/agent run history; extensible Turkish/English i18n.

Foundation: Next.js, Go net/http, PostgreSQL/pgx/sqlc, Redis/Asynq, Centrifugo, Meilisearch, S3-compatible storage, Plate.js, Docker Compose. The canonical document format is versioned Plate JSON. All persistent operations have API and MCP counterparts; pagination/chunking must preserve access to full content. Workspace scope applies to database queries, cache, search, files, live channels and workers.

## Delivery phases

### Phase 0: Documentation and contracts

**How this serves the primary goal:** Establish documented contracts before implementation.

Documentation discovery and version verification precede implementation.

Verification: relevant integration tests, operation/schema coverage and user-visible acceptance criteria below. Guard: no silent truncation, authorization bypass or duplicate mutation on retry.

### Phase 1: Operation registry, access, reliability and workspace isolation

**How this serves the primary goal:** Deliver the corresponding workflow with API/MCP parity and workspace isolation.

Tasks: T01, T02, T03, T04, T05, T41.

Verification: relevant integration tests, operation/schema coverage and user-visible acceptance criteria below. Guard: no silent truncation, authorization bypass or duplicate mutation on retry.

### Phase 2: Teams, projects, product catalog and linked documents

**How this serves the primary goal:** Deliver the corresponding workflow with API/MCP parity and workspace isolation.

Tasks: T06, T07, T43.

Verification: relevant integration tests, operation/schema coverage and user-visible acceptance criteria below. Guard: no silent truncation, authorization bypass or duplicate mutation on retry.

### Phase 3: Issue lifecycle and content

**How this serves the primary goal:** Deliver the corresponding workflow with API/MCP parity and workspace isolation.

Tasks: T08, T09, T10, T11, T12, T47.

Verification: relevant integration tests, operation/schema coverage and user-visible acceptance criteria below. Guard: no silent truncation, authorization bypass or duplicate mutation on retry.

### Phase 4: Web and mobile experience

**How this serves the primary goal:** Deliver the corresponding workflow with API/MCP parity and workspace isolation.

Tasks: T13, T14, T15, T16, T17.

Verification: relevant integration tests, operation/schema coverage and user-visible acceptance criteria below. Guard: no silent truncation, authorization bypass or duplicate mutation on retry.

### Phase 5: Complete MCP surface

**How this serves the primary goal:** Deliver the corresponding workflow with API/MCP parity and workspace isolation.

Tasks: T18, T19, T20, T21, T22, T42.

Verification: relevant integration tests, operation/schema coverage and user-visible acceptance criteria below. Guard: no silent truncation, authorization bypass or duplicate mutation on retry.

### Phase 6: GitHub integration

**How this serves the primary goal:** Deliver the corresponding workflow with API/MCP parity and workspace isolation.

Tasks: T23, T24, T25, T26.

Verification: relevant integration tests, operation/schema coverage and user-visible acceptance criteria below. Guard: no silent truncation, authorization bypass or duplicate mutation on retry.

### Phase 7: Native embedded widget and environment context

**How this serves the primary goal:** Deliver the corresponding workflow with API/MCP parity and workspace isolation.

Tasks: T27, T28, T29, T30, T31, T32, T44.

Verification: relevant integration tests, operation/schema coverage and user-visible acceptance criteria below. Guard: no silent truncation, authorization bypass or duplicate mutation on retry.

### Phase 8: Planning, unified intake, notifications and i18n

**How this serves the primary goal:** Deliver the corresponding workflow with API/MCP parity and workspace isolation.

Tasks: T33, T34, T35, T36, T45, T46.

Verification: relevant integration tests, operation/schema coverage and user-visible acceptance criteria below. Guard: no silent truncation, authorization bypass or duplicate mutation on retry.

### Phase 9: Automation, releases, deploys and agent history

**How this serves the primary goal:** Deliver the corresponding workflow with API/MCP parity and workspace isolation.

Tasks: T48, T49, T50, T51, T52, T53.

Verification: relevant integration tests, operation/schema coverage and user-visible acceptance criteria below. Guard: no silent truncation, authorization bypass or duplicate mutation on retry.

### Phase 10: VPS readiness, backup and widget integration verification

**How this serves the primary goal:** Deliver the corresponding workflow with API/MCP parity and workspace isolation.

Tasks: T37, T38, T39.

Verification: relevant integration tests, operation/schema coverage and user-visible acceptance criteria below. Guard: no silent truncation, authorization bypass or duplicate mutation on retry.

### Phase 11: Whole-product acceptance

**How this serves the primary goal:** Deliver the corresponding workflow with API/MCP parity and workspace isolation.

Tasks: T40.

Verification: relevant integration tests, operation/schema coverage and user-visible acceptance criteria below. Guard: no silent truncation, authorization bypass or duplicate mutation on retry.

## Approved task inventory

Stable task keys support idempotent tracker publication. Task IDs are listed numerically; implementation follows dependency order.

### T01 Ortak işlem envanteri ve API MCP şemalarını kur

Phase 1; depends on: none.

Çalışma alanı, ekip, proje, issue, belge, dosya, görünüm, ürün/ortam, talep, kural, release/deploy, ajan kaydı, ayar ve entegrasyon işlemlerinin tek katalogunu tanımla.

- Her işlem benzersiz kimlik ve giriş/çıkış/hata şemasına sahip
- API ve MCP eşlemesi aynı katalogdan denetleniyor
- Kalıcı kullanıcı tercihleri ve job işlemleri katalogda

### T02 Sahip hesabı ve bağımsız araç erişimini hazırla

Phase 1; depends on: T01.

Mevcut insan oturumunu koru; araç başına iptal edilebilir erişim ve OAuth bağlantı modelini ekle.

- Bir aracın iptali diğer bağlantıyı kesmiyor
- Token süresi, audience ve iptal durumu her çağrıda doğrulanıyor
- İlk kurulumdan sonra uygulama işlemleri panel onayı gerektirmiyor
- Kimlikten ayrı çalışma alanı yetkisi çözülür; seçilen workspace ID tek başına yetki vermez

### T03 Mutation tekrarlarını ve eşzamanlı düzenlemeyi güvenilir yap

Phase 1; depends on: T01.

Idempotency anahtarı, request fingerprint, önceki sonuç replay ve expected version sözleşmesini kur.

- Yarışan aynı create tek kayıt ve aynı sonuç üretiyor
- Aynı key farklı gövdede açık conflict dönüyor
- Eski sürüm yazısı güncel veriyi ezmiyor

### T04 Kalıcı olay alımı ve tüketici teslimatını kur

Phase 1; depends on: T03.

PostgreSQL transaction içinde mutation/geçmiş/outbox; consumer delivery, retry ve replay kur.

- Worker çökünce tamamlanmamış teslimat geri alınabiliyor
- Aynı event iki kez işlense de çift yan etki yok
- Bir tüketicinin hatası diğerlerinin başarı kaydını veya retry durumunu gizlemiyor

### T05 API şema ve MCP işlem kapsamını CI içinde denetle

Phase 1; depends on: T01.

HTTP method/path kapsamını giriş, çıkış, hata şemaları ve MCP işlem kapsamıyla CI içinde karşılaştır.

- Yeni API veya MCP karşılığı eksik işlem CI hatası veriyor
- Eksik HTTP metodu ve şema alanı fixture testinde yakalanıyor
- Generic additionalProperties şeması tam kapsam kabul edilmiyor

### T06 Ekip durum etiket ve issue numara modelini ekle

Status: Done.

Phase 2; depends on: T01, T03, T41.

Ekip kodları, sayaçlar, sıralı workflow ve ortak/ekip etiketlerini geliştir.

- Eşzamanlı numara tahsisi çakışmıyor
- Durum ve etiketler doğru ekip sınırında doğrulanıyor
- API MCP ile ekip ve workflow sıfırdan kurulabiliyor

### T07 Proje kilometre taşı ve doküman düzenini ekle

Status: Done. Source document resources manage links and metadata; versioned Plate documents remain T10/T47.

Phase 2; depends on: T06.

Proje sorumlusu, tarihleri, ekip katkıları, milestone ve kaynak dokümanlarını kur.

- Bir projeye birden fazla ekip katkı yapabiliyor
- Milestone ataması ilgili projeyle doğrulanıyor
- Proje ayarları ve kaynaklar API MCP üzerinden yönetiliyor

### T08 Issue temel yaşam döngüsünü geliştir

Phase 3; depends on: T06, T07, T03, T04.

Başlık, öncelik, proje, milestone, tarih, efor, durum, arşiv ve geri yükleme işlemlerini oluştur.

- Panel API MCP aynı alanları okuyup yazıyor
- Taşıma sonrası eski issue kimliği ve linki çözülüyor
- Değişiklik ve geçmiş aynı transaction ile yazılıyor

### T09 Alt görev ve issue ilişkilerini geliştir

Phase 3; depends on: T08.

Parent/sub-issue, blocks/blocked-by, related ve duplicate ilişkilerini ekle.

- Döngüsel parent ve geçersiz ilişki engelleniyor
- Karşılıklı ilişki okuması tutarlı
- Arşiv taşıma ve geri yükleme ilişki kimliklerini bozmuyor

### T10 Plate.js editörünü sürümlü issue ve proje belgelerine bağla

Phase 3; depends on: T08.

Panel ve widget için ortak Plate.js plugin/node sözleşmesi kur; kanonik sürümlü Plate JSON, gerçek mention/ref ve güvenli HTML türetme ekle.

- Tablo kod kontrol listesi Unicode ve satır sonları round-trip korunuyor
- Belge sürümü ve node doğrulama sunucuda uygulanıyor
- Markdown export farkları açık; tam JSON içerik API MCP ile alınabiliyor
- Bölüm veya node düzenlemesi beklenen revision ile uygulanıyor ve tam belgeyi yeniden yazmadan sürüm bırakıyor
- Panel ve widget aynı Plate.js belge şemasını kullanıyor; Lexical editör temeli taşınmıyor

### T11 Issue ekleri ve tam dosya aktarımını geliştir

Phase 3; depends on: T08, T03.

Attachment UUID, geçici upload, finalize, indirme, metadata ve devamlı listeleme ekle.

- Dosya yükle indir hash eşitliği sağlanıyor
- Yetkisiz veya başka taslağa ait attachment bağlanamıyor
- Eksik upload ve büyük liste kesintisi devam bilgisiyle yönetiliyor
- Çalışma alanı kapsamı giriş, sorgu ve sonuçta korunur; başka alana veri veya dosya sızması test edilir

### T12 Yorum değişiklik geçmişi ve belge revision işlemlerini ekle

Phase 3; depends on: T08, T10, T04.

Yorum, mention, düzenleme kaydı, belge revision ve geri yüklemeyi oluştur.

- Yorum ve belge eski sürümü ayrı okunabiliyor
- Restore yeni revision olarak kaydoluyor
- Panelden ve MCPden yapılan değişiklik aynı zaman çizelgesinde görünüyor

### T13 Arama filtre ve kayıtlı görünüm işlemlerini ekle

Phase 4; depends on: T08.

Issue/proje/ekip arama adapterleri, filtre dili, kararlı sıralama ve kişisel görünüm tercihlerini ekle.

- Filtre sıralama ve sonuç sayısı aynı veri kümesini kullanıyor
- Cursor veya export taraması değişen listede sessiz kayıt atlamıyor
- Görünüm ve kolon tercihi API MCPden değiştirilebiliyor
- Çalışma alanı kapsamı giriş, sorgu ve sonuçta korunur; başka alana veri veya dosya sızması test edilir

### T14 Traccia web kabuğu ve kişisel çalışma ekranını kur

Phase 4; depends on: T06, T07.

Ekip/proje geçişi, bana atananlar, favoriler, bildirim girişi ve komut menüsünü hazırla. Çalışma alanı seçici ve yalnız yetkili alanları açıkça birleştiren kişisel portföy görünümü ekle.

- TR EN ve tema tercihi kalıcı
- Klavye ve odak akışı erişilebilir
- Mevcut auth ve ortak UI bileşenleri kullanılıyor

### T15 Hızlı issue oluşturma ve detay düzenleme deneyimini kur

Phase 4; depends on: T14, T08, T10, T11, T12.

Issue satırı/drawer, hızlı oluşturma, alan seçiciler, yorum ve ekleri birleştir.

- Klavyeyle issue oluşturup düzenleme tamamlanıyor
- Kaydetme hatasında taslak kaybolmuyor
- Canlı güncelleme aktif düzenlemeyi ezmiyor

### T16 Kanban ve toplu issue işlemlerini kur

Phase 4; depends on: T15, T13.

Sürükle bırak durum değişimi, kolon grupları, toplu atama/etiket/proje/durum ve iş sonucu ekle.

- Taşıma sunucu hatasında doğru duruma dönüyor
- Filtreyle tümünü seçme hedefi açıkça gösteriliyor
- Kısmi toplu hata item düzeyinde raporlanıyor ve MCPde okunabiliyor

### T17 Mobil web issue akışlarını tamamla

Phase 4; depends on: T14, T15, T16.

Mobil liste kartları, detay drawer, filtre ve dokunmatik durum değiştirme tasarla.

- Telefon genişliğinde oluşturma düzenleme dosya ekleme tamamlanıyor
- Sadece hover veya drag gerektiren zorunlu eylem yok
- Klavye açılması ve safe area formu erişilemez yapmıyor

### T18 Resmî Go SDK ile MCP transport OAuth ve proxy kur

Phase 5; depends on: T01, T02.

Resmî Go SDK ile Streamable HTTP, metadata, PKCE, rotation, revoke ve Next passthrough kur.

- Gerçek istemciyle keşif bağlantı refresh revoke çalışıyor
- Yanlış audience ve süresi geçmiş token reddediliyor
- MCP protocol ve auth başlıkları doğru aktarılıyor; panel cookie taşınmıyor
- Çalışma alanı kapsamı giriş, sorgu ve sonuçta korunur; başka alana veri veya dosya sızması test edilir

### T19 Çekirdek ekip proje ve issue MCP araçlarını bağla

Phase 5; depends on: T18, T06, T07, T08, T09, T10, T11, T12, T13.

Çekirdek okuma/yazma, ilişki, belge, yorum ve görünüm araçlarını ortak işlemlere bağla.

- Panel açmadan ekip proje issue ve alt görev kurulabiliyor
- Araç giriş/çıkışları katalog şemasıyla doğrulanıyor
- Yazma sonucu PENDING_APPROVAL veya panel linkiyle ertelenmiyor
- Bağlam ve entity keşfi gerçek işlem envanterinden geliyor; toplu çağrı satır başına sonucu raporluyor
- Çalışma alanı kapsamı giriş, sorgu ve sonuçta korunur; başka alana veri veya dosya sızması test edilir

### T20 Ayar tercih bildirim ve entegrasyon yönetimini MCPye aç

Phase 5; depends on: T18, T01.

Uygulamada var olan ayarların ve job işlemlerinin API MCP erişimini kur; sonraki faz işlemleri aynı kayıt düzeniyle eklensin.

- Dil favori görünüm ve bildirim tercihi MCPden değişiyor
- Secret create rotate revoke ve configured durumları aynı politikada
- Kalıcı uygulama ayarının yalnız panele ait karşılığı kalmıyor
- Çalışma alanı kapsamı giriş, sorgu ve sonuçta korunur; başka alana veri veya dosya sızması test edilir

### T21 MCP büyük içerik dosya ve arka plan iş aktarımını tamamla

Phase 5; depends on: T18, T10, T11, T04.

Tam içerik parçaları, kaynak okuma alternatifi, upload finalize/download, export/import job ve cursor erişimini ekle.

- 50den fazla kayıt ve 16 KiBden büyük gövde tamamen alınabiliyor
- MCP upload download byte hash eşit
- Uzun job sonucu bağlantı koptuktan sonra sorgulanabiliyor
- Kaynak okuma ve get aracı aynı yetkiyi kullanıyor; offset kılıfı kararlı snapshot gibi sunulmuyor
- Çalışma alanı kapsamı giriş, sorgu ve sonuçta korunur; başka alana veri veya dosya sızması test edilir

### T22 Panel API MCP eşitliğini uçtan uca doğrula

Phase 5; depends on: T05, T19, T20, T21.

Bütün işlem ailelerinde aynı senaryoyu farklı girişlerden yürütüp alan ve hata karşılaştır.

- Kalıcı alanların tamamı manifest ile ölçülüyor
- Hata conflict ve retry semantiği yüzeyler arasında tutarlı
- Panelsiz tam kullanım senaryosu otomatik testte geçiyor
- İki workspace ile API/MCP/panel scope reddi, cache ve dosya izolasyon testleri geçiyor

### T23 GitHub App kurulum ve repo eşlemelerini ekle

Phase 6; depends on: T02, T07.

GitHub App manifest kurulumu, installation ve repo seçimi, şifreli token saklama ve rate limit yönetimini uygula; çalışma alanı, ekip ve proje eşlemelerine bağla.

- GitHub login ve geliştirme entegrasyonu ayrı yönetiliyor
- App yalnız seçilmiş depoları görüyor
- Kurulum eşlemeleri API MCPden okunup değiştiriliyor
- Çalışma alanı kapsamı giriş, sorgu ve sonuçta korunur; başka alana veri veya dosya sızması test edilir

### T24 GitHub webhook alımını kalıcı ve tekrara dayanıklı yap

Phase 6; depends on: T23, T04.

Ham gövde HMAC doğrulaması, delivery ID, kalıcı inbox, worker, retry ve replay uygula; webhook HTTP başarısını kalıcı alım sonrasında döndür.

- Geçersiz imza işlenmiyor
- Aynı delivery birden fazla yan etki üretmiyor
- Kalıcı alındı olmadan başarılı cevap verilmiyor ve worker hatası izlenebiliyor

### T25 Issue PR eşleştirme ve durum kurallarını geliştir

Phase 6; depends on: T24, T08, T09.

PR/review/check modeli, branch/başlık/gövde/commit referansları ve issue durum kurallarını ekle; abonelik ve uygulanmış handler kapsamını test et.

- Bir issue iki PR gerektiriyorsa ilk merge ile bitmiyor
- Closed without merge Done üretmiyor
- Referans ilişkisi ile kapatan ilişki ayrılıyor; manuel link/unlink API MCPde mevcut

### T26 GitHub uzlaştırma ve geliştirme durum ekranını ekle

Phase 6; depends on: T25, T03.

PR/CI için eksiksiz sayfalama, periyodik/manual sync, kaçan olay uzlaştırması ve geliştirme durum ekranını ekle.

- Eski event yeni durumu geriye almıyor
- Kaçan webhook resync ile düzeliyor
- Resync ve hata/retry geçmişi panel API MCPde görülebiliyor

### T27 Yerleşik widget kurulum modelini ve yönetimini ekle

Phase 7; depends on: T06, T07, T01.

Public installation ID, hedef, ortam, origin, varsayılanlar, dil/görünüm, privacy ve durdurma ayarlarını ekle.

- Aynı origin birden fazla kuruluma sahip olabiliyor
- Duraklatılmış kurulum yeni bildirim kabul etmiyor
- Yanlış ekip projesi ve workflow varsayılanı reddediliyor
- Çalışma alanı kapsamı giriş, sorgu ve sonuçta korunur; başka alana veri veya dosya sızması test edilir
- Kurulum yetkili workspace, ürün ve ortam kaydıyla ilişkilendirilebiliyor

### T28 Widget script ve React SDK deneyimini geliştir

Phase 7; depends on: T27.

Mevcut loader deseninden CSS izolasyonu, lazy iframe, open/close/identify/setContext/destroy ve React bağlayıcısı oluştur.

- Tekrarlı mount tek widget; destroy dinleyicileri temizliyor
- SPA route değişimi bağlamı güncelliyor
- Masaüstü mobil ve odak yönetimi çalışıyor

### T29 Widget ekran görüntüsü görsel ve bağlam akışını kur

Phase 7; depends on: T28, T11.

İsteğe bağlı screenshot, paste/drop/upload, önizleme/kaldırma/karartma ve temizlenmiş sayfa bağlamını ekle.

- Screenshot hatasında dosya yükleme ile devam ediliyor
- Hassas alan ve URL temizleme politikası test ediliyor
- Boyut/adet sınırları ve iki yönlü message correlation uygulanıyor

### T30 Doğrulanmış bildirici ve dar widget oturumunu ekle

Phase 7; depends on: T27, T02.

Host sunucusu doğrulamasıyla kısa ömürlü create-only oturum ve kimlik bağlamı oluştur.

- Public ID veya websiteUrl tek başına yetki vermiyor
- Tam API token tarayıcıya çıkmıyor ve iframe cookieye bağlı değil
- Yetki başka kurulum veya issue okuma için kullanılamıyor

### T31 Widget taslak yükleme ve idempotent gönderim akışını tamamla

Phase 7; depends on: T03, T04, T08, T11, T28, T29, T30.

Native issue creation, upload sahipliği, kalıcı gönderim alındısı, durum sorgusu ve taslak retry ekle.

- Timeout sonrası aynı gönderim tek issue döndürüyor
- Tam başarıdan önce taslak silinmiyor
- Sahipsiz upload temizleniyor; widget akışında Linear API çağrısı yok
- Reddedilen veya kısmi gönderim kuyrukta hata durumuyla korunuyor; ekler sessizce düşürülmüyor

### T32 Widget kurulum ve bildirim araçlarını API MCPye aç

Phase 7; depends on: T20, T27, T31.

Kurulum CRUD, origin/defaults, pause/revoke, snippet, intake ve receipt işlemlerini tam katalogla sun.

- Yalnız MCP ile kurulum açılıp snippet ve test bildirimi alınabiliyor
- Başarı kaydı doğru issueya bağlanıyor
- Panel ayar alanlarının MCP karşılığı kapsam testinde tam
- Ürün/ortam ilişkisinin okuma ve değiştirme işlemleri API/MCP üzerinden tamamlanıyor

### T33 Cycle şablon ve tekrarlayan issue desteğini ekle

Phase 8; depends on: T08, T09, T04.

Ekip dönemleri, efor/kapasite özeti, issue şablonları ve zamanlı tekrarlar oluştur.

- Timezone geçişlerinde iş bir kez oluşuyor
- Dönem taşıma ve şablon alanları doğrulanıyor
- Cycle şablon ve tekrar yaşam döngüsü API MCPde tamam

### T34 Triage bildirim ve takip tercihlerini bağla

Phase 8; depends on: T08, T12, T04.

Gelen işler için kabul/ret/duplicate ve kişisel bildirim takibini issue olaylarına bağla.

- Widget kaynaklı issue doğru ilk duruma gidiyor
- Aynı olay çift bildirim üretmiyor
- Okunma ve takip tercihleri MCPden yönetiliyor

### T35 Proje ilerlemesi ve temel iş raporlarını ekle

Phase 8; depends on: T07, T08, T13.

Proje/milestone ilerlemesi, geciken işler, cycle özeti ve aynı veri için export oluştur.

- Rapor toplamı filtrelenen tam veri kümesinden hesaplanıyor
- Arşiv ve iptal durumlarının etkisi açık
- Rapor ve export jobları API MCPden kullanılabiliyor

### T36 Türkçe İngilizce ve genişletilebilir dil desteğini tamamla

Phase 8; depends on: T10, T14, T28.

Önceki fazların çevirilerini doğrula; katalog kaydı, backend mesajları, tarih/sayı/çoğul ve widget dilini genelleştir.

- TR EN eksik anahtar kontrolü geçiyor
- Üçüncü test dili belgelenen sınırlı kayıt noktalarıyla ekleniyor
- Dil değişimi issue IDlerini API alanlarını ve kalıcı veriyi değiştirmiyor
- Yeni katalog, talep, belge, otomasyon, release ve ajan ekranları da dil kapsam kontrolüne dahil

### T37 VPS Compose proxy ve production env kurulumunu hazırla

Phase 10; depends on: T18, T23, T27.

Sürümlü env üretme/upgrade, health/readiness, migrate sırası ve MCP/GitHub/widget dış yollarını hazırla.

- Yeni kurulum ve upgrade smoke test geçiyor
- Hedef CPU mimarisi build ile uyumlu
- Örnek secretlar reddediliyor ve gerçek secret loglanmıyor
- Çalışma alanı kapsamı giriş, sorgu ve sonuçta korunur; başka alana veri veya dosya sızması test edilir

### T38 Veri dosya yedeği ve geri yükleme provasını hazırla

Phase 10; depends on: T11, T37.

VPS dışı DB+object backup, tutarlı manifest, restore ve bakım runbook oluştur.

- Temiz ortama issue ilişki revision ve dosyalar birlikte dönüyor
- Dosya hashleri backup öncesiyle eşit
- Yedek başarısızlığı ve retention görünür durumda
- Çalışma alanı kapsamı giriş, sorgu ve sonuçta korunur; başka alana veri veya dosya sızması test edilir

### T39 Bağımsız host uygulamalarda widget kabul testlerini kur

Phase 10; depends on: T28, T29, T30, T31, T32.

HTML/SPA ve Next.js fixture hostlarında kurulum, giriş, screenshot, mobil ve retry senaryolarını doğrula.

- Cross-origin ve üçüncü taraf cookie kapalı senaryosu geçiyor
- Bağımsız fixture host uygulamalarıyla doğrulanıyor
- Public IDde secret olmadığı ve sahte mesajın reddi test ediliyor
- Çalışma alanı kapsamı giriş, sorgu ve sonuçta korunur; başka alana veri veya dosya sızması test edilir

### T40 Traccia tam kullanım ve kayıpsızlık kabul paketini çalıştır

Phase 11; depends on: T22, T26, T33, T34, T35, T36, T37, T38, T39, T17, T42, T44, T46, T47, T49, T51, T53.

Panel/API/MCP, GitHub ve widget zincirini büyük veri ve hata senaryolarıyla birlikte doğrula. Onaylanan yedi genişleme için uçtan uca ürün/ortam→talep→issue→PR→release/deploy→otomasyon/ajan izi ve workspace izolasyonu kabul edilir.

- Widget issue bildirimi PR merge ve bildirim zinciri tamam
- 50+ kayıt 16KiB+ gövde 5000+ storage metadata ve dosya hash testi geçiyor
- Go frontend i18n schema parity ve restore kontrolleri raporlanıyor

### T41 Çalışma alanı kapsamını ve veri sınırlarını kur

Phase 1; depends on: T01, T02.

Varsayılan kişisel çalışma alanı → ekip → proje hiyerarşisi; ortak scope resolver, üyelik kontrolü, scoped SQL/composite FK, cache, dosya, arama, job ve canlı kanal sözleşmeleri.

- İki çalışma alanı aynı kullanıcıda ayrı veriyi ve ayarları koruyor
- API/MCP/worker girişinde doğrulanmış scope zorunlu; eksik kapsam güvenli biçimde reddediliyor
- Çapraz workspace parent ilişkileri DB constraint ve servis kontrolüyle reddediliyor
- Görünür rol matrisi veya kiralama/şube modeli eklenmiyor
- Yeni kalıcı işlemler ortak katalog, API/MCP parity ve Türkçe/İngilizce doğrulama kapsamına dahil

### T42 Tenant izolasyon ve iptal kabul paketini kur

Phase 5; depends on: T41, T04, T11, T13, T18.

Çalışma alanı kapsam desenini fixture üzerinden ispatla; cache invalidation, çoklu sekme, job actor iptali, canlı kanal ve dosya sahipliği için semantik izolasyon testleri.

- İki workspace fixture ile HTTP/API/MCP yetkisiz okuma/yazma testleri geçiyor
- Cache invalidation ve kuyrukta bekleyen işin aktör erişimi iptalinde kapsam aşılmıyor
- Dosya, export, arama ve canlı kanal testleri başka alan verisini göstermiyor
- Yeni kalıcı işlemler ortak katalog, API/MCP parity ve Türkçe/İngilizce doğrulama kapsamına dahil

### T43 Ürün ve ortam kataloğunu geliştir

Status: In Progress. Repository and deployment fields are reference metadata; connected GitHub and deployment status remain T23/T50.

Phase 2; depends on: T41, T07, T03.

Ürünü repo, projeler, production/staging/local ortam adresleri ve deploy bağlantılarıyla ilişkilendir; web/mobil CRUD ve arşivleme, aynı ortak işlemlerin API/MCP karşılıkları.

- Ürün ve ortamların tüm yaşam döngüsü panel/API/MCP ile tamamlanıyor
- URL/ortam isimleri workspace kapsamında doğrulanıyor; secret kasası eklenmiyor
- Arşivleme geçmiş ilişkiyi bozmuyor; filtreleme ve devam sayfaları tam veriyi sağlıyor
- Yeni kalıcı işlemler ortak katalog, API/MCP parity ve Türkçe/İngilizce doğrulama kapsamına dahil

### T44 Widget ve issue bağlamını ürün ve ortama bağla

Phase 7; depends on: T43, T27, T30, T31.

Kurulum varsayılanı ürün/ortam/release bağlamını güvenilir sunucu çözümüyle issue kaydına bağla; host kullanıcı bağlamı doğrulanmış kurulum eşlemesini geçersiz kılamaz.

- Aynı ürünün staging ve production bildirimleri ayrılıyor
- Sahte host bağlamı başka workspace veya ürün ilişkisi oluşturamıyor
- Kurulum ve issue bağlamı API/MCP üzerinden tam okunup yönetilebiliyor
- Yeni kalıcı işlemler ortak katalog, API/MCP parity ve Türkçe/İngilizce doğrulama kapsamına dahil

### T45 Ortak talep havuzu ve yönlendirmeyi geliştir

Phase 8; depends on: T08, T34, T43, T03, T04.

Widget, panel, API/MCP ve doğrulanmış webhook girişlerini ortak issue çekirdeği üstünde bug/özellik/fikir/operasyon tipleriyle havuza al; sınıflandır, birleştir ve projeye yönlendir.

- Her kaynağın talebi izlenebilir kaynak ve idempotent kimlikle kaydoluyor
- Benzer talepleri birleştirme kaynak geçmişi ve dosyaları kaybetmiyor
- Yönlendirme/yeniden açma/birleştirme panel/API/MCP eşdeğer sonuç veriyor
- Yeni kalıcı işlemler ortak katalog, API/MCP parity ve Türkçe/İngilizce doğrulama kapsamına dahil

### T46 Talep havuzu deneyimini ve kaynak doğrulamasını tamamla

Phase 8; depends on: T45, T16, T44.

Mobil uyumlu havuz, kaynak/ürün/ortam filtreleri, bulk yönlendirme ve talep→issue bağlantıları; imzalı webhook alımı ile hata/retry yönetimi.

- Widget ve imzalı webhook talepleri projeye yalnız MCP ile yönlendirilebiliyor
- Aynı delivery tekrarında tek talep ve tek issue oluşuyor
- Çapraz workspace merge ve bulk işlem reddi fixture ile test ediliyor
- Yeni kalıcı işlemler ortak katalog, API/MCP parity ve Türkçe/İngilizce doğrulama kapsamına dahil

### T47 Bağlantılı Plate belgeleri ve karar şablonlarını geliştir

Phase 3; depends on: T07, T10, T12.

Proje notu, ADR/teknik karar, runbook ve kontrol listesi şablonlarını ortak sürümlü Plate JSON ile oluştur; issue, PR ve release için typed link/backlink sözleşmesi.

- Belge oluşturma, revision, geri yükleme, ilişki ve backlink işlemleri panel/API/MCP kapsamında
- Issue/PR/release link türleri kimlik ve workspace sınırı doğrulanarak bağlanıyor
- Büyük Plate JSON ve özel node içeriği tam round-trip testiyle korunuyor
- Yeni kalıcı işlemler ortak katalog, API/MCP parity ve Türkçe/İngilizce doğrulama kapsamına dahil

### T48 Olay koşul eylem otomasyon çekirdeğini kur

Phase 9; depends on: T04, T08, T43, T45, T25.

Hazır olay/koşul/eylem seti, kural sürümü, devreye alma/durdurma ve dry-run önizleme; ortak servisleri kullanan kalıcı yürütme modeli.

- Production bug→öncelik/ekip yönlendirme hazır kuralı çalışıyor
- Kural preview ve yönetimi panel/API/MCP ile aynı planı üretiyor
- Eylem izinleri çalışma anında doğrulanıyor; keyfi kod çalıştırılmıyor
- Yeni kalıcı işlemler ortak katalog, API/MCP parity ve Türkçe/İngilizce doğrulama kapsamına dahil

### T49 Otomasyon tekrar döngü ve hata yönetimini tamamla

Phase 9; depends on: T48, T03.

Olay/kurala bağlı idempotency, recursion/depth limiti, cooldown, kalıcı çalıştırma geçmişi, retry ve hata inceleme.

- Tek olay retry edildiğinde yan etki çoğalmıyor
- Birbirini tetikleyen kurallar belirlenen sınırda duruyor
- Başarı/hata/atlanan koşul ve retry geçmişi API/MCP üzerinden eksiksiz sorgulanıyor
- Yeni kalıcı işlemler ortak katalog, API/MCP parity ve Türkçe/İngilizce doğrulama kapsamına dahil

### T50 Release ve deploy veri akışını geliştir

Phase 9; depends on: T24, T25, T43, T04.

Issue→PR→commit→release→ortam/deploy ilişkisi; GitHub deployment/status/release olaylarını kalıcı inbox üzerinden ilişkilendir; deploy durumu merge durumundan ayrılır.

- PR merge edilmesi production deploy olarak gösterilmiyor
- Sırasız veya yinelenen deploy/status olayları güncel durumu geriletmiyor
- Release/deploy ilişki ve durumları panel/API/MCP ile tam okunup yönetiliyor
- Yeni kalıcı işlemler ortak katalog, API/MCP parity ve Türkçe/İngilizce doğrulama kapsamına dahil

### T51 Release notları ve ortam görünürlüğünü tamamla

Phase 9; depends on: T50, T47, T48.

Sürüm notlarını Plate belgeleriyle ilişkilendir; ortamda çalışan sürüm, ilgili issue/PR, deploy ve rollback bağlantılarını mobil uyumlu sun; otomasyon koşullarına deploy sonucunu ekle.

- Bir issue hangi ortam ve sürümde yayında sorusu cevaplanıyor
- Release notu ve rollback bağlantısı API/MCP üzerinden yönetiliyor
- PR merge + başarılı production deploy kuralı fixture ile doğrulanıyor; otomatik merge veya VPS kontrol paneli eklenmiyor
- Yeni kalıcı işlemler ortak katalog, API/MCP parity ve Türkçe/İngilizce doğrulama kapsamına dahil

### T52 MCP ve ajan işlem geçmişini ilişkilendir

Phase 9; depends on: T18, T01, T04, T08.

İstemci, aktör, workspace, operation, correlation/run ID, durum, bağlı issue/PR ve güvenli hata bilgisiyle kalıcı run/event kaydı; mevcut ajan araçlarını izle, yeni ajan runtime kurma.

- İstemcinin run correlation ID alanı varsa birden fazla araç çağrısı aynı run altında bağlanıyor
- Secret ve ham tokenlar kayda alınmıyor; actor/workspace sınırı korunuyor
- Run ve event kayıtları devam sayfası ve tam içerik referansıyla API/MCP üzerinden okunabiliyor
- Yeni kalıcı işlemler ortak katalog, API/MCP parity ve Türkçe/İngilizce doğrulama kapsamına dahil

### T53 Ajan run ekranı ve güvenli retry izini tamamla

Phase 9; depends on: T52, T03, T21.

Run durumu, hata, issue/PR ilişkileri ve retry zincirini web/mobil göster; retry aynı yetki/idempotency kurallarıyla orijinal işleme gider; süresi dolan/yarıda kalan kayıtları uzlaştır.

- Retry yeni bir yan etkiyi yanlışlıkla çoğaltmıyor; üst işlem/deneme ilişkisi korunuyor
- Panelsiz istemci run oluşturma, ilişkilendirme, sonuç bildirme ve geçmiş okumayı tamamlıyor
- Crash/timeout ve erişim iptali testleri açık sonuç üretiyor
- Yeni kalıcı işlemler ortak katalog, API/MCP parity ve Türkçe/İngilizce doğrulama kapsamına dahil

## Boundaries

No commercial billing, Slack/Teams integration, enterprise permission matrix, native mobile app, agent execution runtime or arbitrary workflow code execution. The widget uses the native issue service. Automatic console/network capture and session replay are outside the initial approved implementation. Production deployment depends on configured infrastructure.
