# Quick Start - Load Testing

## TL;DR

```bash
# 1. Preparar sistema
./load_tests/scripts/prepare_system.ps1  # Windows
# ou
sudo ./load_tests/scripts/prepare_system.sh  # Linux/macOS

# 2. Subir stack
docker compose -f docker-compose.yml -f docker-compose.loadtest.yml up -d

# 3. Criar tenants
cd load_tests
python utils/tenant_factory.py --create --count 50

# 4. Instalar deps
pip install -r requirements.txt

# 5. Rodar teste
python quick_start.py baseline
```

## Cenários Disponíveis

| Cenário | Comando | Duração | Objetivo |
|---------|---------|---------|----------|
| Baseline | `python quick_start.py baseline` | 10min | Estabelecer baseline (100 RPS) |
| Throughput | `python quick_start.py throughput` | 30min | Medir throughput máximo (até 5000 RPS) |
| WebSocket Scale | `python quick_start.py websocket_scale` | 60min | Testar 10k conexões WebSocket |
| Multi-tenant | `python quick_start.py multi_tenant` | 30min | Validar isolamento entre tenants |
| Stress | `python quick_start.py stress` | 60min | Encontrar breaking point |

## Monitoramento

- **Locust UI**: http://localhost:8089
- **Grafana**: http://localhost:3000 (admin/admin)
- **Prometheus**: http://localhost:9090
- **Jaeger**: http://localhost:16686

## Verificar Setup

```bash
cd load_tests
chmod +x scripts/verify_setup.sh
./scripts/verify_setup.sh
```

## Métricas Críticas

Monitorar durante o teste:

1. **Consumer Lag** (mais crítico!)
   - Saudável: < 100
   - Atenção: 100-500
   - Crítico: > 1000

2. **Taxa de Erro**
   - Saudável: < 1%
   - Atenção: 1-5%
   - Crítico: > 5%

3. **Latência p95**
   - Boa: < 200ms
   - Aceitável: 200-500ms
   - Lenta: > 500ms

## Troubleshooting

### Tenant pool empty
```bash
python utils/tenant_factory.py --create --count 50
```

### Consumer lag alto
```bash
docker compose up -d --scale worker=8
```

### WebSocket connection errors
```bash
# Windows: Docker Desktop -> Settings -> Resources
# Linux/macOS:
ulimit -n 65536
```

## Documentação Completa

Veja [`load_tests/README.md`](README.md) para documentação detalhada.
