package main

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	// Se preferir o chromedp, descomente:
	// "github.com/chromedp/cdproto/page"
	// "github.com/chromedp/chromedp"

	// Usando Selenium (tebeka)
	"github.com/tebeka/selenium"
)

// --------------------------------------------------------------------------------
// CONFIG
// --------------------------------------------------------------------------------

type Config struct {
	TesseractCmd       string
	Website            string
	TempoEspera        time.Duration
	Debug              bool
	SaidaCSV           string
	MonitorCSV         string
	MonitorIntervalo   int
	MonitorVariacao    int
	OutputFolder       string
	ChromeDriverFolder string
}

var config = Config{
	TesseractCmd:       `C:\Program Files\Tesseract-OCR\tesseract.exe`,
	Website:            "https://www.ligapokemon.com.br/",
	TempoEspera:        4 * time.Second,
	Debug:              false,
	SaidaCSV:           "resultados_final.csv",
	MonitorCSV:         "monitor_registros.csv",
	MonitorIntervalo:   60,
	MonitorVariacao:    30,
	OutputFolder:       "",
	ChromeDriverFolder: "",
}

// --------------------------------------------------------------------------------
// ESTRUTURAS DE DADOS
// --------------------------------------------------------------------------------

// Estrutura para representar uma carta no CSV de entrada
type CardInput struct {
	Nome    string `json:"nome"`
	Colecao string `json:"colecao"`
	Numero  string `json:"numero"`
}

// Estrutura para representar resultados do scraping
type CardResult struct {
	Nome       string  `json:"nome"`
	Colecao    string  `json:"colecao"`
	Numero     string  `json:"numero"`
	Condicao   string  `json:"condicao"`
	Quantidade int     `json:"quantidade"`
	Preco      float64 `json:"preco"`
	PrecoTotal float64 `json:"preco_total"`
	Lingua     string  `json:"lingua"`
}

// Estrutura para monitoramento
type MonitorEntry struct {
	Nome         string  `json:"nome"`
	Colecao      string  `json:"colecao"`
	Numero       string  `json:"numero"`
	PrecoAtual   float64 `json:"preco_atual"`
	DataAtual    string  `json:"data_atual"`
	PrecoInicial float64 `json:"preco_inicial"`
	DataInicial  string  `json:"data_inicial"`
}

// --------------------------------------------------------------------------------
// VARIÁVEIS GLOBAIS (p/ MONITORAMENTO)
// --------------------------------------------------------------------------------

var (
	monitorRunning bool
	monitorPaused  bool
	monitorMutex   sync.Mutex
	wgMonitor      sync.WaitGroup
)

// --------------------------------------------------------------------------------
// FUNÇÕES AUXILIARES DE CSV
// --------------------------------------------------------------------------------

// Carrega uma lista de cartas (nome, colecao, numero) a partir de um CSV
func carregarListaCardsCSV(caminhoCSV string) ([]CardInput, error) {
	var lista []CardInput

	f, err := os.Open(caminhoCSV)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.Comma = ';'
	// Detecta se na primeira linha tem ',' e muda dinamicamente
	firstLineRaw, err := reader.Read()
	if err != nil {
		return nil, err
	}
	// Se na primeira linha tiver ',' em vez de ';', reabrimos com esse separador
	if len(firstLineRaw) == 1 && strings.Contains(firstLineRaw[0], ",") {
		f.Close()
		f2, err2 := os.Open(caminhoCSV)
		if err2 != nil {
			return nil, err2
		}
		defer f2.Close()
		reader = csv.NewReader(f2)
		reader.Comma = ','
		firstLineRaw, err = reader.Read()
		if err != nil {
			return nil, err
		}
	}

	// Precisamos mapear as colunas: "nome", "colecao", "numero"
	colIndex := make(map[string]int)
	for i, col := range firstLineRaw {
		col = strings.TrimSpace(strings.ToLower(col))
		colIndex[col] = i
	}

	reqCols := []string{"nome", "colecao", "numero"}
	for _, rc := range reqCols {
		if _, ok := colIndex[rc]; !ok {
			return nil, fmt.Errorf("coluna '%s' ausente no CSV", rc)
		}
	}

	// Lê as linhas
	lines, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	for _, line := range lines {
		if len(line) < len(colIndex) {
			continue
		}
		nome := strings.TrimSpace(line[colIndex["nome"]])
		colecao := strings.TrimSpace(line[colIndex["colecao"]])
		numero := strings.TrimSpace(line[colIndex["numero"]])
		if nome == "" || colecao == "" || numero == "" {
			continue
		}
		lista = append(lista, CardInput{
			Nome:    nome,
			Colecao: colecao,
			Numero:  numero,
		})
	}

	return lista, nil
}

// Salva resultados em CSV (append ou cria novo)
func salvarResultadosCSV(resultados []CardResult, caminhoSaida string) error {
	if len(resultados) == 0 {
		fmt.Println("[AVISO] Nenhum resultado para salvar.")
		return nil
	}

	colunas := []string{
		"nome", "colecao", "numero",
		"condicao", "quantidade", "preco",
		"preco_total", "lingua",
	}

	existe := false
	if _, err := os.Stat(caminhoSaida); err == nil {
		existe = true
	}
	var file *os.File
	var err error
	if existe {
		file, err = os.OpenFile(caminhoSaida, os.O_APPEND|os.O_WRONLY, 0644)
	} else {
		file, err = os.Create(caminhoSaida)
	}
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	writer.Comma = ';'

	if !existe {
		writer.Write(colunas)
	}
	for _, r := range resultados {
		record := []string{
			r.Nome,
			r.Colecao,
			r.Numero,
			r.Condicao,
			strconv.Itoa(r.Quantidade),
			fmt.Sprintf("%.2f", r.Preco),
			fmt.Sprintf("%.2f", r.PrecoTotal),
			r.Lingua,
		}
		writer.Write(record)
	}
	writer.Flush()
	return nil
}

// Limpar CSV
func limparCSV(caminho string) {
	os.Remove(caminho)
}

// --------------------------------------------------------------------------------
// FUNÇÕES DE DOWNLOAD E SETUP DO CHROMEDRIVER
// --------------------------------------------------------------------------------

func getLatestChromeDriverVersion() (string, error) {
	url := "https://googlechromelabs.github.io/chrome-for-testing/last-known-good-versions-with-downloads.json"
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("status code: %d", resp.StatusCode)
	}
	body, _ := ioutil.ReadAll(resp.Body)
	// Você pode usar json.Unmarshal, mas de forma rápida:
	s := string(body)
	// Pega a linha "Stable" - simplisticamente
	// (Em produção, parseie com struct certinho.)
	idx := strings.Index(s, `"Stable"`)
	if idx < 0 {
		return "", errors.New("não encontrou 'Stable' no JSON")
	}
	versChunk := s[idx:]
	idxVer := strings.Index(versChunk, `"version"`)
	versChunk = versChunk[idxVer:]
	idxDquote := strings.Index(versChunk, `"`)
	versChunk = versChunk[idxDquote+1:]
	idxDquote2 := strings.Index(versChunk, `"`)
	final := versChunk[:idxDquote2]
	return final, nil
}

func downloadChromeDriver() (string, error) {
	fmt.Println("[INFO] Buscando versão mais recente do ChromeDriver...")
	version, err := getLatestChromeDriverVersion()
	if err != nil {
		return "", err
	}
	if version == "" {
		return "", errors.New("não foi possível obter a versão do ChromeDriver")
	}
	systemOS := runtime.GOOS
	// Monta URL
	var downloadURL string
	switch systemOS {
	case "windows":
		downloadURL = fmt.Sprintf("https://storage.googleapis.com/chrome-for-testing-public/%s/win64/chromedriver-win64.zip", version)
	case "linux":
		downloadURL = fmt.Sprintf("https://storage.googleapis.com/chrome-for-testing-public/%s/linux64/chromedriver-linux64.zip", version)
	case "darwin":
		downloadURL = fmt.Sprintf("https://storage.googleapis.com/chrome-for-testing-public/%s/mac-x64/chromedriver-mac-x64.zip", version)
	default:
		return "", fmt.Errorf("sistema operacional não suportado: %s", systemOS)
	}

	zipPath := filepath.Join(".", "chromedriver.zip")
	fmt.Printf("[INFO] Baixando ChromeDriver v%s de %s...\n", version, downloadURL)

	resp, err := http.Get(downloadURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("falha no download: status %d", resp.StatusCode)
	}

	outZip, err := os.Create(zipPath)
	if err != nil {
		return "", err
	}
	defer outZip.Close()

	_, err = io.Copy(outZip, resp.Body)
	if err != nil {
		return "", err
	}
	outZip.Close()

	// Descompacta
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		os.Remove(zipPath)
		return "", fmt.Errorf("erro ao abrir zip: %v", err)
	}
	defer r.Close()

	extractPath := filepath.Join(".", "chromedriver_temp")
	os.MkdirAll(extractPath, 0755)

	var extractedFile string
	for _, f := range r.File {
		fp := filepath.Join(extractPath, f.Name)
		if f.FileInfo().IsDir() {
			os.MkdirAll(fp, f.Mode())
			continue
		}
		os.MkdirAll(filepath.Dir(fp), 0755)
		outF, err2 := os.OpenFile(fp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err2 != nil {
			return "", err2
		}
		rc, err3 := f.Open()
		if err3 != nil {
			outF.Close()
			return "", err3
		}
		_, err4 := io.Copy(outF, rc)
		rc.Close()
		outF.Close()
		if err4 != nil {
			return "", err4
		}
		if strings.Contains(strings.ToLower(f.Name), "chromedriver") {
			extractedFile = fp
		}
	}

	os.Remove(zipPath)
	if extractedFile == "" {
		return "", errors.New("chromedriver não encontrado após extração")
	}

	finalPath := filepath.Join(".", "chromedriver")
	if systemOS == "windows" {
		finalPath = filepath.Join(".", "chromedriver.exe")
	}
	os.Remove(finalPath) // remove anterior, se existir

	err = os.Rename(extractedFile, finalPath)
	if err != nil {
		return "", err
	}
	// Torna executável
	if systemOS != "windows" {
		os.Chmod(finalPath, 0755)
	}

	os.RemoveAll(extractPath)
	fmt.Printf("[INFO] ChromeDriver pronto em: %s\n", finalPath)
	return finalPath, nil
}

// Verifica se já existe o ChromeDriver local e, caso não, baixa.
func checkAndDownloadChromeDriver() (string, error) {
	systemOS := runtime.GOOS
	var driverPath string
	if systemOS == "windows" {
		driverPath = filepath.Join(".", "chromedriver.exe")
	} else {
		driverPath = filepath.Join(".", "chromedriver")
	}

	if _, err := os.Stat(driverPath); err == nil {
		fmt.Printf("[INFO] ChromeDriver já existe em %s\n", driverPath)
		return driverPath, nil
	}
	fmt.Println("[INFO] ChromeDriver não encontrado, iniciando download...")
	return downloadChromeDriver()
}

// --------------------------------------------------------------------------------
// FUNÇÕES DE SCRAPING (exemplo com Selenium + ChromeDriver)
// --------------------------------------------------------------------------------

// Abre o ChromeDriver usando o Selenium
func iniciarSelenium(driverPath string) (selenium.WebDriver, func(), error) {
	// Setup das capacidades
	const port = 9515
	opts := []selenium.ServiceOption{}
	selenium.SetDebug(config.Debug)
	service, err := selenium.NewChromeDriverService(driverPath, port, opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("erro ao iniciar serviço do ChromeDriver: %v", err)
	}

	cleanup := func() {
		service.Stop()
	}

	caps := selenium.Capabilities{"browserName": "chrome"}
	if !config.Debug {
		// Modo headless
		chromeCaps := map[string]interface{}{
			"args": []string{
				"--headless",
				"--disable-gpu",
				"--no-sandbox",
			},
		}
		caps["goog:chromeOptions"] = chromeCaps
	}

	wd, err := selenium.NewRemote(caps, fmt.Sprintf("http://localhost:%d/wd/hub", port))
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("erro ao criar sessão selenium: %v", err)
	}
	return wd, cleanup, nil
}

// Função simplificada de scrape, baseada nas suas funções Python
func buscaCartaCompleta(wd selenium.WebDriver, nome, colecao, numero string) ([]CardResult, error) {
	resultados := []CardResult{}

	// Monta URL
	nomeUrl := strings.ReplaceAll(nome, " ", "%20")
	url := fmt.Sprintf("%s?view=cards/card&card=%s%%20(%s)&ed=%s&num=%s",
		config.Website, nomeUrl, numero, colecao, numero)
	err := wd.Get(url)
	if err != nil {
		return resultados, err
	}
	time.Sleep(config.TempoEspera)

	// Fecha banner cookies (tentativa)
	fechaBannerCookies(wd)

	// Tenta localizar #marketplace-stores
	storesContainer, err := wd.FindElement(selenium.ByCSSSelector, "#marketplace-stores")
	if err != nil {
		fmt.Println("[AVISO] Sem marketplace-stores. Nenhum vendedor encontrado.")
		return resultados, nil
	}
	stores, err := storesContainer.FindElements(selenium.ByCSSSelector, ".store")
	if err != nil {
		return resultados, nil
	}

	for _, store := range stores {
		lingua, cond := extraiLinguaECondicao(store)
		if strings.Contains(strings.ToUpper(cond), "NM") {
			btnComprar, err := localizaBotaoComprarNM(store)
			if err == nil && btnComprar != nil {
				// Clica comprar
				fechaBannerCookies(wd)
				btnComprar.Click()
				time.Sleep(1 * time.Second)
				abreModalCarrinho(wd)
				rowCarrinho, err2 := localizaItemCarrinho(wd, nome, numero)
				if err2 == nil && rowCarrinho != nil {
					dados, _ := extraiDadosItemCarrinho(rowCarrinho)
					dados.Nome = nome
					dados.Colecao = colecao
					dados.Numero = numero
					dados.Lingua = lingua
					dados.Condicao = cond
					resultados = append(resultados, dados)

					removeItemCarrinho(rowCarrinho)
					break // se já achou 1, pode sair
				}
			}
		}
	}

	return resultados, nil
}

// Fecha banner cookies
func fechaBannerCookies(wd selenium.WebDriver) {
	banner, err := wd.FindElement(selenium.ByCSSSelector, "#lgpd-cookie")
	if err == nil {
		btn, err2 := banner.FindElement(selenium.ByTagName, "button")
		if err2 == nil {
			btn.Click()
			time.Sleep(time.Second)
			fmt.Println("[INFO] Banner de cookies fechado.")
		}
	}
}

// Extrai língua e condição
func extraiLinguaECondicao(store selenium.WebElement) (string, string) {
	lingua := ""
	condicao := ""
	infos, err := store.FindElement(selenium.ByCSSSelector, ".infos-quality-and-language.desktop-only")
	if err != nil {
		return lingua, condicao
	}
	imgs, _ := infos.FindElements(selenium.ByTagName, "img")
	for _, img := range imgs {
		title, _ := img.GetAttribute("title")
		if title != "" {
			lingua = title
		}
	}
	qs, _ := infos.FindElements(selenium.ByCSSSelector, ".quality")
	for _, q := range qs {
		title, _ := q.GetAttribute("title")
		if strings.Contains(strings.ToUpper(title), "NM") {
			condicao = title
			break
		}
	}
	return lingua, condicao
}

// Localiza botão comprar
func localizaBotaoComprarNM(store selenium.WebElement) (selenium.WebElement, error) {
	btn, err := store.FindElement(selenium.ByCSSSelector, "div.btn-green.cursor-pointer")
	if err != nil {
		return nil, err
	}
	return btn, nil
}

// Abre modal carrinho
func abreModalCarrinho(wd selenium.WebDriver) {
	iconeCarrinho, err := wd.FindElement(selenium.ByCSSSelector, "div.cart-icon-container.icon-container")
	if err == nil {
		iconeCarrinho.Click()
		time.Sleep(time.Second)
		meuCarrinhoBtn, err2 := wd.FindElement(selenium.ByCSSSelector, "a.btn-view-cart")
		if err2 == nil {
			meuCarrinhoBtn.Click()
			time.Sleep(config.TempoEspera)
		}
	}
}

// Localiza item no carrinho
func localizaItemCarrinho(wd selenium.WebDriver, nome, numero string) (selenium.WebElement, error) {
	itens, err := wd.FindElement(selenium.ByCSSSelector, "div.itens")
	if err != nil {
		return nil, err
	}
	rows, err := itens.FindElements(selenium.ByCSSSelector, "div.row")
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		tituloElem, err := row.FindElement(selenium.ByCSSSelector, "p.cardtitle a")
		if err != nil {
			continue
		}
		texto, _ := tituloElem.Text()
		if strings.Contains(texto, nome) && strings.Contains(texto, "("+numero+")") {
			return row, nil
		}
	}
	return nil, errors.New("item não encontrado")
}

// Extrai dados do item no carrinho
func extraiDadosItemCarrinho(row selenium.WebElement) (CardResult, error) {
	dados := CardResult{}
	estoqueElem, err := row.FindElement(selenium.ByCSSSelector, "div.item-estoque")
	if err == nil {
		txt, _ := estoqueElem.Text()
		dados.Quantidade = parseEstoque(txt)
	}
	precoElem, err2 := row.FindElement(selenium.ByCSSSelector, "div.preco-total.item-total")
	if err2 == nil {
		txt, _ := precoElem.Text()
		val := convertePrecoParaFloat(txt)
		dados.Preco = val
		dados.PrecoTotal = val
	}
	return dados, nil
}

// Remove item do carrinho
func removeItemCarrinho(row selenium.WebElement) {
	btnRemove, err := row.FindElement(selenium.ByCSSSelector, "div.btn-circle.remove.delete.item-delete")
	if err == nil {
		btnRemove.Click()
		time.Sleep(1 * time.Second)
	}
}

func parseEstoque(txt string) int {
	parts := strings.Fields(txt)
	for _, p := range parts {
		if n, err := strconv.Atoi(p); err == nil {
			return n
		}
	}
	return 0
}

func convertePrecoParaFloat(txt string) float64 {
	txt = strings.ReplaceAll(txt, "R$", "")
	txt = strings.ReplaceAll(txt, ".", "")
	txt = strings.ReplaceAll(txt, ",", ".")
	txt = strings.TrimSpace(txt)
	val, _ := strconv.ParseFloat(txt, 64)
	return val
}

// --------------------------------------------------------------------------------
// FUNÇÕES DE MONITORAMENTO EM BACKGROUND
// --------------------------------------------------------------------------------

// Executa monitoramento em loop
func monitorLoop(lista []CardInput) {
	defer wgMonitor.Done()
	checkCount := 0
	for {
		monitorMutex.Lock()
		if !monitorRunning {
			monitorMutex.Unlock()
			fmt.Println("[MONITOR] finalizado.")
			return
		}
		if monitorPaused {
			monitorMutex.Unlock()
			time.Sleep(1 * time.Second)
			continue
		}
		monitorMutex.Unlock()

		checkCount++
		fmt.Printf("[MONITOR] Checagem #%d para %d cartas.\n", checkCount, len(lista))

		// Abre Selenium
		driverPath, err := checkAndDownloadChromeDriver()
		if err != nil {
			fmt.Printf("[MONITOR] ERRO download chromedriver: %v\n", err)
			time.Sleep(10 * time.Second)
			continue
		}
		wd, cleanup, err := iniciarSelenium(driverPath)
		if err != nil {
			fmt.Printf("[MONITOR] ERRO iniciar selenium: %v\n", err)
			time.Sleep(10 * time.Second)
			continue
		}

		var resultsMonitor []CardResult

		for i, card := range lista {
			percent := int((float64(i+1) / float64(len(lista))) * 100)
			fmt.Printf("[MONITOR] %s (%s - %s): %d%%\n", card.Nome, card.Colecao, card.Numero, percent)

			ret, err2 := buscaCartaCompleta(wd, card.Nome, card.Colecao, card.Numero)
			if err2 == nil && len(ret) > 0 {
				resultsMonitor = append(resultsMonitor, ret...)
				precoAtual := ret[0].Preco
				dtStr := time.Now().Format("2006-01-02 15:04:05")
				_ = salvarMonitoramento(card.Nome, card.Colecao, card.Numero, precoAtual, dtStr, filepath.Join(config.OutputFolder, config.MonitorCSV))
				fmt.Printf("[MONITOR] %s preco %.2f\n", card.Nome, precoAtual)
			} else {
				fmt.Printf("[MONITOR] NM não encontrado p/ %s\n", card.Nome)
			}
		}
		wd.Quit()
		cleanup()

		if len(resultsMonitor) > 0 {
			_ = salvarResultadosCSV(resultsMonitor, filepath.Join(config.OutputFolder, config.SaidaCSV))
		}

		// Calcula tempo de espera
		tempoBase := config.MonitorIntervalo
		variacao := config.MonitorVariacao
		espera := tempoBase + rand.Intn(variacao)

		for s := 0; s < espera; s++ {
			monitorMutex.Lock()
			if !monitorRunning {
				monitorMutex.Unlock()
				return
			}
			if monitorPaused {
				monitorMutex.Unlock()
				time.Sleep(1 * time.Second)
				s--
				continue
			}
			monitorMutex.Unlock()
			time.Sleep(1 * time.Second)
		}
	}
}

// Salva no CSV de monitoramento (semelhante ao seu Python)
func salvarMonitoramento(nome, colecao, numero string, preco float64, dataStr, caminho string) error {
	colunas := []string{
		"nome", "colecao", "numero", "preco_atual",
		"data_atual", "preco_inicial", "data_inicial",
	}
	existe := false
	if _, err := os.Stat(caminho); err == nil {
		existe = true
	}
	// Se existe, carregamos p/ ver se já tinha registro
	var dfExistente []MonitorEntry
	if existe {
		dfExistente, _ = carregarMonitorCSV(caminho)
	}

	precoFloat := preco
	idx := -1
	for i, me := range dfExistente {
		if me.Nome == nome && me.Colecao == colecao && me.Numero == numero {
			idx = i
			break
		}
	}
	if idx != -1 {
		// atualiza
		if dfExistente[idx].PrecoInicial == 0 {
			dfExistente[idx].PrecoInicial = precoFloat
			dfExistente[idx].DataInicial = dataStr
		}
		dfExistente[idx].PrecoAtual = precoFloat
		dfExistente[idx].DataAtual = dataStr
		// Sobrescreve CSV
		return sobrescreverMonitorCSV(caminho, dfExistente, colunas)
	} else {
		// adiciona
		f, err := os.OpenFile(caminho, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return err
		}
		defer f.Close()

		writer := csv.NewWriter(f)
		writer.Comma = ';'
		if !existe {
			writer.Write(colunas)
		}
		record := []string{
			nome,
			colecao,
			numero,
			fmt.Sprintf("%.2f", precoFloat),
			dataStr,
			fmt.Sprintf("%.2f", precoFloat),
			dataStr,
		}
		writer.Write(record)
		writer.Flush()
		fmt.Printf("[INFO] Monitoramento salvo p/ %s (%.2f)\n", nome, precoFloat)
	}
	return nil
}

func carregarMonitorCSV(caminho string) ([]MonitorEntry, error) {
	var lista []MonitorEntry
	f, err := os.Open(caminho)
	if err != nil {
		return lista, err
	}
	defer f.Close()
	reader := csv.NewReader(f)
	reader.Comma = ';'
	cols, err := reader.Read()
	if err != nil {
		return lista, err
	}
	colIndex := make(map[string]int)
	for i, c := range cols {
		colIndex[strings.ToLower(strings.TrimSpace(c))] = i
	}
	lines, err2 := reader.ReadAll()
	if err2 != nil {
		return lista, err2
	}
	for _, line := range lines {
		if len(line) < len(cols) {
			continue
		}
		var me MonitorEntry
		me.Nome = line[colIndex["nome"]]
		me.Colecao = line[colIndex["colecao"]]
		me.Numero = line[colIndex["numero"]]
		me.PrecoAtual, _ = strconv.ParseFloat(line[colIndex["preco_atual"]], 64)
		me.DataAtual = line[colIndex["data_atual"]]
		me.PrecoInicial, _ = strconv.ParseFloat(line[colIndex["preco_inicial"]], 64)
		me.DataInicial = line[colIndex["data_inicial"]]
		lista = append(lista, me)
	}
	return lista, nil
}

func sobrescreverMonitorCSV(caminho string, lista []MonitorEntry, colunas []string) error {
	f, err := os.Create(caminho)
	if err != nil {
		return err
	}
	defer f.Close()
	writer := csv.NewWriter(f)
	writer.Comma = ';'
	writer.Write(colunas)
	for _, me := range lista {
		rec := []string{
			me.Nome,
			me.Colecao,
			me.Numero,
			fmt.Sprintf("%.2f", me.PrecoAtual),
			me.DataAtual,
			fmt.Sprintf("%.2f", me.PrecoInicial),
			me.DataInicial,
		}
		writer.Write(rec)
	}
	writer.Flush()
	return nil
}

// --------------------------------------------------------------------------------
// HANDLERS DA API (HTTP)
// --------------------------------------------------------------------------------

// GET /ping - só para teste
func pingHandler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("pong"))
}

// POST /scrape - recebe JSON com lista de CardInput e faz scraping
type ScrapeRequest struct {
	Cards []CardInput `json:"cards"`
}

func scrapeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Use POST para /scrape", http.StatusMethodNotAllowed)
		return
	}
	var req ScrapeRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, fmt.Sprintf("erro parse JSON: %v", err), http.StatusBadRequest)
		return
	}
	if len(req.Cards) == 0 {
		http.Error(w, "Nenhuma carta enviada", http.StatusBadRequest)
		return
	}
	driverPath, err := checkAndDownloadChromeDriver()
	if err != nil {
		http.Error(w, fmt.Sprintf("erro no chromedriver: %v", err), http.StatusInternalServerError)
		return
	}
	wd, cleanup, err := iniciarSelenium(driverPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("erro iniciar selenium: %v", err), http.StatusInternalServerError)
		return
	}
	defer wd.Quit()
	defer cleanup()

	var resultados []CardResult
	for _, c := range req.Cards {
		ret, err2 := buscaCartaCompleta(wd, c.Nome, c.Colecao, c.Numero)
		if err2 == nil && len(ret) > 0 {
			resultados = append(resultados, ret...)
		}
	}
	if len(resultados) > 0 {
		outCSV := filepath.Join(config.OutputFolder, config.SaidaCSV)
		salvarResultadosCSV(resultados, outCSV)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resultados)
}

// POST /monitor - inicia (ou retoma) o monitoramento em background
// Recebe JSON com cards p/ monitorar
func monitorHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Use POST para /monitor", http.StatusMethodNotAllowed)
		return
	}
	var req ScrapeRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, fmt.Sprintf("erro parse JSON: %v", err), http.StatusBadRequest)
		return
	}
	if len(req.Cards) == 0 {
		http.Error(w, "Nenhuma carta enviada", http.StatusBadRequest)
		return
	}

	monitorMutex.Lock()
	if monitorRunning {
		monitorMutex.Unlock()
		w.Write([]byte("Monitor já está em execução.\n"))
		return
	}
	monitorRunning = true
	monitorPaused = false
	monitorMutex.Unlock()

	wgMonitor.Add(1)
	go monitorLoop(req.Cards)

	w.Write([]byte("Monitoramento iniciado.\n"))
}

// POST /monitor/pause - pausa ou retoma o monitor
func monitorPauseHandler(w http.ResponseWriter, r *http.Request) {
	monitorMutex.Lock()
	defer monitorMutex.Unlock()
	if !monitorRunning {
		w.Write([]byte("Monitor não está em execução.\n"))
		return
	}
	monitorPaused = !monitorPaused
	if monitorPaused {
		w.Write([]byte("Monitor pausado.\n"))
	} else {
		w.Write([]byte("Monitor retomado.\n"))
	}
}

// GET /monitor/stop - interrompe monitor
func monitorStopHandler(w http.ResponseWriter, r *http.Request) {
	monitorMutex.Lock()
	if monitorRunning {
		monitorRunning = false
	}
	monitorMutex.Unlock()

	w.Write([]byte("Monitor interrompido.\n"))
}

// GET /clean - limpa histórico CSV
func cleanHandler(w http.ResponseWriter, r *http.Request) {
	outCSV := filepath.Join(config.OutputFolder, config.SaidaCSV)
	limparCSV(outCSV)
	w.Write([]byte("Histórico de raspagem removido.\n"))
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("API rodando! Use as rotas corretamente. Exemplo: /ping"))
}

// --------------------------------------------------------------------------------
// MAIN
// --------------------------------------------------------------------------------

func main() {
	// Ajuste se quiser forçar uma pasta de saída
	config.OutputFolder, _ = os.Getwd()

	mux := http.NewServeMux()
	mux.HandleFunc("/ping", pingHandler)
	mux.HandleFunc("/", homeHandler) // Página inicial
	mux.HandleFunc("/scrape", scrapeHandler)
	mux.HandleFunc("/monitor", monitorHandler)
	mux.HandleFunc("/monitor/pause", monitorPauseHandler)
	mux.HandleFunc("/monitor/stop", monitorStopHandler)
	mux.HandleFunc("/clean", cleanHandler)

	srv := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	fmt.Println("API rodando em http://localhost:8080 ... (Ctrl+C para sair)")

	// Executa servidor em goroutine
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Erro ao iniciar servidor: %v", err)
		}
	}()

	// Aguardar interrupção ctrl+C
	esperarInterrupcao()

	// Se monitor estiver rodando, avisa e aguarda
	monitorMutex.Lock()
	if monitorRunning {
		monitorRunning = false
	}
	monitorMutex.Unlock()
	wgMonitor.Wait()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	fmt.Println("Servidor finalizado.")
}

// --------------------------------------------------------------------------------
// Espera Interrupção (Ctrl+C)
// --------------------------------------------------------------------------------
func esperarInterrupcao() {
	c := make(chan os.Signal, 1)
	// signal.Notify(c, syscall.SIGINT, syscall.SIGTERM) // se quiser importar "os/signal" e "syscall"
	fmt.Println("Pressione Ctrl+C para interromper... (no Windows, feche a janela)")
	<-c
}
