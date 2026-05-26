# CROM Image Compression & SQL Delivery System

Este projeto implementa um ecossistema completo e inovador para compressão, armazenamento e entrega distribuída de imagens usando a tecnologia **CROM (Content-Defined Chunking + Vector Quantization)** de quantização vetorial espacial, persistência estruturada em SQLite e decodificação client-side de alto desempenho direto no navegador do usuário utilizando HTML5 Canvas.

---

## 🚀 Como Funciona o Sistema?

Em sistemas web tradicionais, carregar imagens consome uma quantidade massiva de tráfego de rede, pois o servidor envia os arquivos compactados (JPEG, PNG, WebP) inteiros para cada cliente.

Este projeto propõe uma alternativa baseada em **Quantização Vetorial**:
1. **Codebook Único ("Brain")**: O navegador baixa um dicionário binário (`codebook.cromdb`) uma única vez e o armazena localmente em cache.
2. **Payloads Levíssimos**: As imagens no banco de dados SQLite consistem apenas de metadados mínimos e um array de índices de 16 bits (`uint16`). Cada bloco de pixel $8 \times 8$ da imagem original é mapeado para uma única palavra de 2 bytes no codebook (gerando uma compressão física de **96x** sobre os pixels RGB brutos!).
3. **Decodificação Client-side**: Utilizando o decodificador JavaScript integrado em [static/app.js](static/app.js), o navegador renderiza as imagens realizando consultas ultra-rápidas $O(1)$ na memória e desenhando diretamente no canvas através de `putImageData`.

---

## 📁 Estrutura do Repositório

```
├── main.go                      # Ponto de entrada CLI (Train, Compress, Benchmark, Server)
├── pkg/
│   ├── compressor/              # Pipeline de segmentação, compressão e reconstrução em Go
│   ├── database/                # Inicialização e operações do banco de dados SQLite
│   └── server/                  # Servidor web HTTP e rotas de API REST
├── static/                      # Interface Web Premium do Dashboard
│   ├── index.html               # Página principal do Dashboard
│   ├── style.css                # Estilização moderna e responsiva (Dark theme)
│   └── app.js                   # Decodificador CROM em JS de alta performance
├── tests/                       # Nova suíte de testes automatizados
│   ├── run_tests.sh             # Script principal para rodar testes e compilar resultados
│   ├── unit/                    # Testes unitários do compressor e banco de dados
│   └── integration/             # Testes de rotas e fluxo HTTP ponta a ponta
├── go.mod                       # Módulos do Go (com replace para crompressor local)
└── README.md                    # Este arquivo
```

---

## 🛠️ Instalação e Execução

### Requisitos
* Go 1.22 ou superior
* GCC (caso use SQLite nativo, porém o projeto utiliza SQLite em pure Go `modernc.org/sqlite` dispensando CGO)

### Passos para Configuração

1. **Compilar o Projeto:**
   ```bash
   go build -o crompressor-sql-image main.go
   ```

2. **Treinar o Codebook (Criação do Cérebro):**
   Para treinar o dicionário visual a partir de imagens de exemplo. Se nenhuma pasta for indicada, o CLI baixará fotos reais de Picsum Photos e gerará gradientes sintéticos para compilar um dataset de 100MB:
   ```bash
   ./crompressor-sql-image cli train --output codebook.cromdb --block-size 8 --max-words 8192
   ```

3. **Rodar os Benchmarks:**
   Compactar um diretório inteiro de imagens e analisar a taxa de redução contra formatos comuns (JPEG, WebP, Base64), salvando os resultados no SQLite (`images.db`):
   ```bash
   ./crompressor-sql-image cli benchmark --input ./training_dataset --codebook codebook.cromdb --db images.db
   ```

4. **Iniciar o Servidor e Dashboard:**
   Para visualizar a interface do usuário:
   ```bash
   ./crompressor-sql-image server --port 8080 --codebook codebook.cromdb --db images.db
   ```
   Abra seu navegador em [http://localhost:8080](http://localhost:8080).

---

## 📊 Dashboard Web Premium

A interface web fornece ferramentas interativas para testar o sistema:
* **Upload Integrado**: Faça upload de qualquer PNG/JPEG para que o backend realize a compressão em CROM em tempo real e insira no SQLite.
* **Comparação Visual**: Veja o arquivo JPEG original lado a lado com a versão decodificada no navegador por JavaScript.
* **Métricas de Qualidade**: O navegador calcula e atualiza dinamicamente o **MSE** (Erro Quadrático Médio) e o **PSNR** (Fidelidade Visual em dB).
* **Gráfico de Largura de Banda**: Um gráfico interativo mostra a economia real gerada pelo sistema no segundo acesso (com o codebook já cacheado no navegador).

---

## 🧪 Suíte de Testes Automatizada

O projeto conta com uma suíte de testes robusta na pasta `/tests` que valida todas as partes fundamentais do sistema e gera relatórios automáticos.

Para rodar todos os testes e compilar os logs e relatórios executáveis:
```bash
chmod +x tests/run_tests.sh
./tests/run_tests.sh
```

Os resultados serão armazenados em:
* `tests/results/unit_tests.log` - Saída dos testes unitários de compressão e banco de dados.
* `tests/results/integration_tests.log` - Saída dos testes de endpoints HTTP mocks.
* `tests/results/report.md` - Um relatório consolidado com tempos de execução, status e estatísticas.

---

## 📐 Fórmulas Matemáticas Utilizadas

### Taxa de Compressão Espacial (Blocos $8 \times 8$)
Cada bloco de pixels bruto consome:
$$\text{Tamanho Original} = 8 \times 8 \times 3 \text{ (RGB)} = 192 \text{ bytes}$$

Cada bloco compactado é representado por apenas 1 índice do codebook:
$$\text{Tamanho Comprimido} = 1 \times \text{uint16} = 2 \text{ bytes}$$
$$\text{Redução Física} = \frac{192}{2} = 96\times \text{ (Economia de 98.96\% de dados)}$$

### Relação Sinal-Ruído de Pico (PSNR)
Para medir a fidelidade visual dos blocos gerados a partir do dicionário em relação aos pixels reais:
$$MSE = \frac{1}{3 \cdot W \cdot H} \sum_{y=0}^{H-1} \sum_{x=0}^{W-1} \sum_{c \in \{R,G,B\}} (I_{orig}(x,y,c) - I_{recon}(x,y,c))^2$$
$$PSNR = 10 \cdot \log_{10}\left(\frac{255^2}{MSE}\right)$$
*(Valores de PSNR acima de 30 dB indicam excelente qualidade de imagem, quase imperceptível ao olho humano).*
