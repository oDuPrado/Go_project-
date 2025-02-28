## 🌟 Visão Geral

O **Scraper API** é uma solução backend desenvolvida em Go para coletar e monitorar preços de cartas Pokémon em tempo real.  
  
✅ **Scraping automatizado:** Utiliza Selenium e ChromeDriver para extrair dados diretamente do site da Liga Pokémon.  
✅ **Monitoramento contínuo:** Permite agendar checagens e atualizar preços periodicamente.  
✅ **API REST:** Disponibiliza os dados extraídos via endpoints simples, facilitando a integração com outros sistemas.

---

## 🚀 2️⃣ Visão Geral do Sistema

O projeto é construído sobre três pilares fundamentais:

### 📂 1. Coleta de Dados
- **Download automático do ChromeDriver:** Se o driver não existir, o sistema baixa, descompacta e configura o ambiente automaticamente.
- **Scraping com Selenium:** Abre o site, fecha banners de cookies e extrai informações das cartas com condição NM.

### 🛠 2. Armazenamento Local
- **Histórico em CSV:** Salva os resultados de cada scraping em um arquivo CSV para consultas futuras e monitoramento.

### 🌐 3. Exposição via API
- **Endpoints REST:** Cria um servidor HTTP que disponibiliza os dados coletados através de rotas como `/scrape`, `/monitor` e `/ping`.

---

## 🎯 3️⃣ Funcionalidades do Sistema

### 🔍 Coleta e Exibição de Dados
- Recebe um JSON com os dados de entrada (nome, coleção e número da carta).
- Realiza o scraping e extrai informações como preço, quantidade, condição e língua.

### 💾 Armazenamento Persistente
- Registra os resultados em arquivos CSV, possibilitando o acompanhamento do histórico de buscas.

### 🌐 API REST
- **Endpoints Disponíveis:**
  - `GET /ping` → Testa a disponibilidade da API.
  - `POST /scrape` → Envia um JSON com as cartas e retorna os dados extraídos.
  - `POST /monitor` → Inicia o monitoramento contínuo dos preços.
  - `POST /monitor/pause` → Pausa ou retoma o monitoramento.
  - `GET /monitor/stop` → Interrompe o monitoramento.
  - `GET /clean` → Limpa o histórico de resultados.

---

## 🚀 4️⃣ Tecnologias Utilizadas

<div align="center">
  <h3>Tecnologias</h3>
  <img src="https://cdn.jsdelivr.net/gh/devicons/devicon/icons/go/go-original.svg" height="50" alt="Golang" /> &nbsp;
  <img src="https://img.shields.io/badge/Selenium-tebeka/selenium-4E9CAF?style=for-the-badge" height="50" alt="Selenium" /> &nbsp;
  <img src="https://img.shields.io/badge/HTTP-net%2Fhttp-007ACC?style=for-the-badge&logo=http&logoColor=white" height="50" alt="net/http" /> &nbsp;
  <img src="https://img.shields.io/badge/JSON-encoding%2Fjson-4B8BBE?style=for-the-badge&logo=json&logoColor=white" height="50" alt="encoding/json" />
</div>

---

## 🖥️ Getting Started

### 🔧 Pré-requisitos
Antes de começar, certifique-se de ter instalado:
- **Go (1.16+):** [Download Go](https://go.dev/dl/)
- **Chrome:** Necessário para o ChromeDriver funcionar corretamente.
- **Dependências Go:**  
  Execute:
  ```bash
  go get github.com/tebeka/selenium

---

## 📜 **Licença**

🔓 Este projeto está licenciado sob a **[MIT License](LICENSE)**, permitindo o uso, modificação e distribuição sob os termos da licença MIT.  
 
---

## Contato

Caso tenha dúvidas, sugestões ou queira contribuir, entre em contato:

<div align="center">
  <a href="https://www.linkedin.com/in/marcoauréliomacedoprado" target="_blank">
    <img src="https://img.shields.io/static/v1?message=LinkedIn&logo=linkedin&label=&color=0077B5&logoColor=white&labelColor=&style=plastic" height="36" alt="linkedin logo" />
  </a>
  <a href="https://www.instagram.com/prado.marco1/" target="_blank">
    <img src="https://img.shields.io/static/v1?message=Instagram&logo=instagram&label=&color=E4405F&logoColor=white&labelColor=&style=plastic" height="36" alt="instagram logo" />
  </a>
  <a href="https://wa.me/5567996893356" target="_blank">
  <img src="https://img.shields.io/static/v1?message=Whatsapp&logo=whatsapp&label=&color=25D366&logoColor=white&labelColor=&style=plastic" height="36" alt="whatsapp logo" />
</a>
  <a href="https://discord.com/users/yourdiscordid" target="_blank">
    <img src="https://img.shields.io/static/v1?message=Discord&logo=discord&label=&color=7289DA&logoColor=white&labelColor=&style=plastic" height="36" alt="discord logo" />
  </a>
</div>
