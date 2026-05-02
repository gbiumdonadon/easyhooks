"""Metrics collector for load tests."""
import time
from typing import Optional
from dataclasses import dataclass, field
from collections import defaultdict
import json


@dataclass
class MetricPoint:
    """Single metric measurement."""
    timestamp: float
    value: float
    labels: dict = field(default_factory=dict)


class MetricsCollector:
    """
    Collect custom metrics during load testing.
    
    This complements Locust's built-in metrics with custom measurements
    like end-to-end latency, message delivery time, etc.
    """
    
    def __init__(self):
        self.metrics = defaultdict(list)
        self.counters = defaultdict(int)
        self.gauges = defaultdict(float)
    
    def record_histogram(self, name: str, value: float, labels: Optional[dict] = None):
        """
        Record a histogram metric (e.g., latency, duration).
        
        Args:
            name: Metric name
            value: Measured value
            labels: Optional labels dict
        """
        point = MetricPoint(
            timestamp=time.time(),
            value=value,
            labels=labels or {}
        )
        self.metrics[name].append(point)
    
    def increment_counter(self, name: str, value: int = 1, labels: Optional[dict] = None):
        """
        Increment a counter metric.
        
        Args:
            name: Metric name
            value: Increment amount
            labels: Optional labels dict
        """
        key = self._make_key(name, labels)
        self.counters[key] += value
    
    def set_gauge(self, name: str, value: float, labels: Optional[dict] = None):
        """
        Set a gauge metric (current value).
        
        Args:
            name: Metric name
            value: Current value
            labels: Optional labels dict
        """
        key = self._make_key(name, labels)
        self.gauges[key] = value
    
    def _make_key(self, name: str, labels: Optional[dict] = None) -> str:
        """Create a unique key from name and labels."""
        if not labels:
            return name
        label_str = ",".join(f"{k}={v}" for k, v in sorted(labels.items()))
        return f"{name}{{{label_str}}}"
    
    def get_histogram_stats(self, name: str) -> dict:
        """
        Calculate statistics for a histogram metric.
        
        Args:
            name: Metric name
            
        Returns:
            Dict with min, max, avg, p50, p95, p99
        """
        if name not in self.metrics:
            return {}
        
        values = sorted(point.value for point in self.metrics[name])
        if not values:
            return {}
        
        def percentile(data, p):
            k = (len(data) - 1) * p
            f = int(k)
            c = f + 1
            if c >= len(data):
                return data[-1]
            d0 = data[f] * (c - k)
            d1 = data[c] * (k - f)
            return d0 + d1
        
        return {
            "count": len(values),
            "min": min(values),
            "max": max(values),
            "avg": sum(values) / len(values),
            "p50": percentile(values, 0.50),
            "p95": percentile(values, 0.95),
            "p99": percentile(values, 0.99),
        }
    
    def export_json(self, filename: str):
        """
        Export all metrics to JSON file.
        
        Args:
            filename: Output file path
        """
        data = {
            "histograms": {
                name: {
                    "stats": self.get_histogram_stats(name),
                    "samples": len(points)
                }
                for name, points in self.metrics.items()
            },
            "counters": dict(self.counters),
            "gauges": dict(self.gauges),
        }
        
        with open(filename, 'w') as f:
            json.dump(data, f, indent=2)
    
    def print_summary(self):
        """Print metrics summary to console."""
        print("\n" + "=" * 80)
        print("CUSTOM METRICS SUMMARY")
        print("=" * 80)
        
        if self.metrics:
            print("\nHistograms:")
            for name in sorted(self.metrics.keys()):
                stats = self.get_histogram_stats(name)
                print(f"\n  {name}:")
                print(f"    Count: {stats['count']}")
                print(f"    Min:   {stats['min']:.2f}")
                print(f"    Avg:   {stats['avg']:.2f}")
                print(f"    P50:   {stats['p50']:.2f}")
                print(f"    P95:   {stats['p95']:.2f}")
                print(f"    P99:   {stats['p99']:.2f}")
                print(f"    Max:   {stats['max']:.2f}")
        
        if self.counters:
            print("\nCounters:")
            for key, value in sorted(self.counters.items()):
                print(f"  {key}: {value}")
        
        if self.gauges:
            print("\nGauges:")
            for key, value in sorted(self.gauges.items()):
                print(f"  {key}: {value:.2f}")
        
        print("\n" + "=" * 80 + "\n")
    
    def reset(self):
        """Clear all metrics."""
        self.metrics.clear()
        self.counters.clear()
        self.gauges.clear()


# Global collector instance
collector = MetricsCollector()
