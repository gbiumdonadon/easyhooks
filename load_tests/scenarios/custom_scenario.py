"""
Cenário customizado - configure seus parâmetros de teste aqui.

Este arquivo permite configurar facilmente todos os parâmetros do seu teste
em um único lugar, sem precisar editar múltiplos arquivos.
"""
from locust import HttpUser, task, between, LoadTestShape
import sys
from pathlib import Path

sys.path.append(str(Path(__file__).parent.parent))
from locustfile import WebhookUser, WebSocketUser


# ============================================
# 🎯 CONFIGURAÇÃO DOS PARÂMETROS
# ============================================

# 1. TENANTS
# Crie o pool antes de executar o teste:
# python load_tests/utils/tenant_factory.py --create --count <TENANT_COUNT>
TENANT_COUNT = 50  # Número de tenants no pool (referência - crie manualmente)

# 2. USUÁRIOS VIRTUAIS
MAX_USERS = 200  # Total de usuários virtuais simultâneos
RAMP_UP_TIME = 120  # Tempo para chegar ao máximo (segundos)
STEADY_STATE_TIME = 600  # Tempo mantendo carga máxima (segundos)

# 3. PROPORÇÃO WEBHOOK vs WEBSOCKET
WEBHOOK_WEIGHT = 7  # Peso dos usuários que enviam webhooks (70% se total=10)
WEBSOCKET_WEIGHT = 3  # Peso dos usuários que ouvem via WebSocket (30% se total=10)

# 4. TAXA DE WEBHOOKS (intervalo entre requisições)
WEBHOOK_WAIT_MIN = 0.5  # Intervalo mínimo entre webhooks (segundos)
WEBHOOK_WAIT_MAX = 1.5  # Intervalo máximo entre webhooks (segundos)
# Quanto menor o intervalo, maior a taxa de webhooks/segundo

# 5. COMPORTAMENTO WEBSOCKET
WEBSOCKET_WAIT_MIN = 2  # Intervalo mínimo de polling (segundos)
WEBSOCKET_WAIT_MAX = 5  # Intervalo máximo de polling (segundos)

# ============================================
# 📊 IMPLEMENTAÇÃO (não precisa editar)
# ============================================

class CustomWebhookUser(WebhookUser):
    """
    Usuário que envia webhooks via HTTP POST.
    """
    wait_time = between(WEBHOOK_WAIT_MIN, WEBHOOK_WAIT_MAX)
    weight = WEBHOOK_WEIGHT


class CustomWebSocketUser(WebSocketUser):
    """
    Usuário que mantém conexão WebSocket e recebe eventos.
    """
    wait_time = between(WEBSOCKET_WAIT_MIN, WEBSOCKET_WAIT_MAX)
    weight = WEBSOCKET_WEIGHT


class CustomLoadShape(LoadTestShape):
    """
    Define o formato de carga ao longo do tempo.
    
    Fases:
    1. Ramp up: 0 → MAX_USERS gradualmente
    2. Steady state: Mantém MAX_USERS constante
    3. Fim: Para o teste
    """
    
    def tick(self):
        run_time = self.get_run_time()
        
        # Fase 1: Ramp up
        if run_time < RAMP_UP_TIME:
            user_count = int(run_time / RAMP_UP_TIME * MAX_USERS)
            spawn_rate = max(1, MAX_USERS // RAMP_UP_TIME)
            return (user_count, spawn_rate)
        
        # Fase 2: Steady state
        elif run_time < (RAMP_UP_TIME + STEADY_STATE_TIME):
            return (MAX_USERS, 1)
        
        # Fase 3: Fim do teste
        else:
            return None


# ============================================
# 📈 CÁLCULOS E ESTIMATIVAS
# ============================================

def print_test_estimates():
    """Imprime estimativas do teste baseado nos parâmetros configurados."""
    total_weight = WEBHOOK_WEIGHT + WEBSOCKET_WEIGHT
    webhook_users = MAX_USERS * WEBHOOK_WEIGHT / total_weight
    websocket_users = MAX_USERS * WEBSOCKET_WEIGHT / total_weight
    
    avg_wait = (WEBHOOK_WAIT_MIN + WEBHOOK_WAIT_MAX) / 2
    webhooks_per_second = webhook_users / avg_wait
    
    total_duration = RAMP_UP_TIME + STEADY_STATE_TIME
    steady_webhooks = webhooks_per_second * STEADY_STATE_TIME
    ramp_webhooks = (webhooks_per_second / 2) * RAMP_UP_TIME  # Média durante ramp up
    total_webhooks = steady_webhooks + ramp_webhooks
    
    print("=" * 70)
    print("🎯 ESTIMATIVAS DO TESTE CUSTOMIZADO")
    print("=" * 70)
    print()
    print(f"📦 TENANTS")
    print(f"   Pool de tenants necessário: {TENANT_COUNT}")
    print(f"   Comando: python load_tests/utils/tenant_factory.py --create --count {TENANT_COUNT}")
    print()
    print(f"👥 USUÁRIOS")
    print(f"   Usuários totais: {MAX_USERS}")
    print(f"   ├─ Enviando webhooks: ~{int(webhook_users)} ({WEBHOOK_WEIGHT}/{total_weight} = {webhook_users/MAX_USERS*100:.0f}%)")
    print(f"   └─ Ouvindo WebSocket: ~{int(websocket_users)} ({WEBSOCKET_WEIGHT}/{total_weight} = {websocket_users/MAX_USERS*100:.0f}%)")
    print()
    print(f"📨 WEBHOOKS")
    print(f"   Taxa de webhooks: ~{int(webhooks_per_second)}/segundo (steady state)")
    print(f"   Intervalo entre webhooks: {WEBHOOK_WAIT_MIN}s - {WEBHOOK_WAIT_MAX}s (média: {avg_wait}s)")
    print(f"   Total de webhooks: ~{int(total_webhooks):,}")
    print()
    print(f"🔌 WEBSOCKETS")
    print(f"   Conexões WebSocket: ~{int(websocket_users)} persistentes")
    print(f"   Mensagens recebidas via WS: ~{int(total_webhooks):,} (se todos os eventos forem entregues)")
    print()
    print(f"⏱️  DURAÇÃO")
    print(f"   Duração total: {total_duration}s ({total_duration // 60} minutos)")
    print(f"   ├─ Ramp up: {RAMP_UP_TIME}s ({RAMP_UP_TIME // 60} minutos)")
    print(f"   └─ Steady state: {STEADY_STATE_TIME}s ({STEADY_STATE_TIME // 60} minutos)")
    print()
    print(f"💾 DADOS ESTIMADOS")
    print(f"   Volume de dados (assumindo ~500 bytes/webhook): ~{int(total_webhooks * 500 / 1024 / 1024)} MB")
    print()
    print("=" * 70)
    print()
    print("📋 COMANDOS PARA EXECUTAR:")
    print()
    print("   # 1. Criar tenants (se ainda não existirem)")
    print(f"   python load_tests/utils/tenant_factory.py --create --count {TENANT_COUNT}")
    print()
    print("   # 2. Executar teste")
    print("   locust -f load_tests/scenarios/custom_scenario.py --host=http://localhost:8000")
    print()
    print("   # 3. Acessar interface web")
    print("   http://localhost:8089")
    print()
    print("=" * 70)


if __name__ == "__main__":
    print_test_estimates()
