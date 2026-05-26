#!/bin/bash

# Ensure we exit on error
set -e

# Path setup
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
RESULTS_DIR="$ROOT_DIR/tests/results"

# Create directories
mkdir -p "$RESULTS_DIR"

echo "=========================================="
echo " Starting CROM SQL Image Test Suite"
echo "=========================================="
echo "Root directory: $ROOT_DIR"
echo "Results directory: $RESULTS_DIR"
echo ""

# Track overall success
SUCCESS=true
START_TIME=$(date +%s)

# 1. Run Unit Tests
echo "Running Unit Tests..."
set +e
go test -v "$ROOT_DIR/tests/unit" > "$RESULTS_DIR/unit_tests.log" 2>&1
UNIT_STATUS=$?
set -e

if [ $UNIT_STATUS -eq 0 ]; then
    echo "  [PASS] Unit Tests completed successfully."
else
    echo "  [FAIL] Unit Tests failed. Check tests/results/unit_tests.log"
    SUCCESS=false
fi

# 2. Run Integration Tests
echo "Running Integration Tests..."
set +e
go test -v -timeout 30m "$ROOT_DIR/tests/integration" > "$RESULTS_DIR/integration_tests.log" 2>&1
INTEGRATION_STATUS=$?
set -e

if [ $INTEGRATION_STATUS -eq 0 ]; then
    echo "  [PASS] Integration Tests completed successfully."
else
    echo "  [FAIL] Integration Tests failed. Check tests/results/integration_tests.log"
    SUCCESS=false
fi

END_TIME=$(date +%s)
DURATION=$((END_TIME - START_TIME))

# 3. Compile Consolidated Markdown Report
REPORT_FILE="$RESULTS_DIR/report.md"
echo "Compiling consolidated markdown report..."

cat << EOF > "$REPORT_FILE"
# Relatório de Execução da Suíte de Testes

Este relatório consolida os resultados da execução dos testes automatizados para o sistema **CROM Image Compression & SQL Delivery**.

---

## 📈 Resumo Geral

* **Status Global:** $( [ "$SUCCESS" = true ] && echo "🟢 PASSANDO" || echo "🔴 FALHANDO" )
* **Data de Execução:** $(date -u +"%Y-%m-%d %H:%M:%S UTC")
* **Tempo de Execução:** ${DURATION} segundos

---

## 🔬 Detalhes das Suítes

### 1. Testes Unitários (tests/unit)
* **Status:** $( [ $UNIT_STATUS -eq 0 ] && echo "🟢 PASS" || echo "🔴 FAIL" )
* **Log:** [unit_tests.log](file://$RESULTS_DIR/unit_tests.log)
* **Componentes Validados:**
  * Pipeline de segmentação de imagens de dimensões arbitrárias (ex: 15x19 pixels) com recorte dinâmico (crop).
  * Lógica matemática de cálculo de métricas de distorção visual (**MSE** e **PSNR**) para imagens iguais e ligeiramente diferentes.
  * Inicialização e CRUD de banco de dados SQLite em memória com chaves primárias e UPSERT (\`INSERT OR REPLACE\`).

### 2. Testes de Integração (tests/integration)
* **Status:** $( [ $INTEGRATION_STATUS -eq 0 ] && echo "🟢 PASS" || echo "🔴 FAIL" )
* **Log:** [integration_tests.log](file://$RESULTS_DIR/integration_tests.log)
* **API REST Validada (Rotas Mock):**
  * \`GET /api/codebook\`: Disponibilização e consistência do arquivo do dicionário visual.
  * \`POST /api/images\`: Upload de imagem multipart, compressão pelo CROM em tempo real e persistência automática no SQLite.
  * \`GET /api/images\`: Listagem JSON com metadados e tamanhos comparados de largura de banda.
  * \`GET /api/images/{id}\`: Obtenção do payload CROM de índices (verificação de tamanho do blob).
  * \`GET /api/images/{id}/original\`: Download do JPEG original a partir do Base64 decodificado.
  * \`DELETE /api/images/{id}\`: Exclusão física do banco e consistência de integridade referencial.

---

## 🛠️ Notas de Execução e Logs de Suporte

* Se houver falhas, você pode inspecionar os arquivos de log anexados diretamente na pasta:
  * Log Unitário: \`tests/results/unit_tests.log\`
  * Log de Integração: \`tests/results/integration_tests.log\`
EOF

echo ""
echo "=========================================="
if [ "$SUCCESS" = true ]; then
    echo " STATUS: SUCCESS"
else
    echo " STATUS: FAILED"
fi
echo " Duration: ${DURATION}s"
echo " Report generated at: tests/results/report.md"
echo "=========================================="
