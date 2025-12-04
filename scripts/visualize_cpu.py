#!/usr/bin/env python3
"""
CPU Utilization Visualization Across Test Variants
Generates ASCII and detailed comparison charts
"""

import csv
import sys
from collections import defaultdict

def load_cpu_data(filename):
    """Load CPU data from CSV file"""
    cpu_data = []
    with open(filename, 'r') as f:
        reader = csv.DictReader(f)
        for row in reader:
            try:
                cpu_data.append(float(row['cpu_percent']))
            except (ValueError, KeyError):
                continue
    return cpu_data

def calculate_stats(data):
    """Calculate statistics from data"""
    if not data:
        return {}
    
    sorted_data = sorted(data)
    n = len(sorted_data)
    
    return {
        'min': sorted_data[0],
        'max': sorted_data[-1],
        'mean': sum(data) / n,
        'median': sorted_data[n // 2],
        'p95': sorted_data[int(n * 0.95)],
        'p99': sorted_data[int(n * 0.99)] if n > 1 else sorted_data[-1],
    }

def generate_ascii_chart(datasets, labels, width=80, height=20):
    """Generate ASCII chart comparing multiple datasets"""
    if not datasets:
        return "No data"
    
    # Find global min/max
    all_data = [val for dataset in datasets for val in dataset]
    global_min = min(all_data)
    global_max = max(all_data)
    value_range = global_max - global_min
    
    if value_range == 0:
        value_range = 1
    
    # Generate chart
    chart = []
    chart.append("=" * width)
    chart.append(f"CPU Utilization Comparison - {len(datasets)} Test Variants")
    chart.append("=" * width)
    chart.append("")
    
    # Labels
    symbols = ['█', '▓', '░']
    for i, label in enumerate(labels):
        chart.append(f"{symbols[i % len(symbols)]} = {label}")
    chart.append("")
    chart.append(f"Y-axis: CPU % ({global_min:.1f}% - {global_max:.1f}%)")
    chart.append(f"X-axis: Time (samples over 2 minutes)")
    chart.append("=" * width)
    chart.append("")
    
    # Create visualization grid
    max_len = max(len(dataset) for dataset in datasets)
    grid = [[' ' for _ in range(width)] for _ in range(height)]
    
    # Plot each dataset
    for dataset_idx, data in enumerate(datasets):
        symbol = symbols[dataset_idx % len(symbols)]
        step = len(data) / width if len(data) > width else 1
        
        for x in range(min(width, len(data))):
            idx = int(x * step) if step > 1 else x
            if idx >= len(data):
                break
            
            normalized_value = (data[idx] - global_min) / value_range
            y = int((height - 1) - (normalized_value * (height - 1)))
            
            if 0 <= y < height:
                if grid[y][x] == ' ':
                    grid[y][x] = symbol
    
    # Print grid with Y-axis labels
    for i, row in enumerate(grid):
        if i == 0:
            y_val = global_max
        elif i == height - 1:
            y_val = global_min
        else:
            y_val = global_max - (i / (height - 1)) * value_range
        
        chart.append(f"{y_val:6.1f}% |{''.join(row)}|")
    
    chart.append(" " * 8 + "-" * width)
    chart.append(" " * 8 + "0" + " " * (width - 20) + f"{max_len}s")
    chart.append("")
    
    return '\n'.join(chart)

def generate_statistics_table(stats_list, labels):
    """Generate statistics comparison table"""
    lines = []
    lines.append("")
    lines.append("=" * 90)
    lines.append("CPU UTILIZATION STATISTICS")
    lines.append("=" * 90)
    lines.append("")
    lines.append(f"{'Scenario':<25} {'Min %':<10} {'Mean %':<10} {'Median %':<10} {'P95 %':<10} {'P99 %':<10} {'Max %':<10}")
    lines.append("-" * 90)
    
    for label, stats in zip(labels, stats_list):
        lines.append(
            f"{label:<25} "
            f"{stats['min']:<10.2f} "
            f"{stats['mean']:<10.2f} "
            f"{stats['median']:<10.2f} "
            f"{stats['p95']:<10.2f} "
            f"{stats['p99']:<10.2f} "
            f"{stats['max']:<10.2f}"
        )
    
    lines.append("=" * 90)
    lines.append("")
    
    return '\n'.join(lines)

def generate_comparison_insights(stats_list, labels):
    """Generate insights from comparison"""
    lines = []
    lines.append("=" * 90)
    lines.append("KEY INSIGHTS")
    lines.append("=" * 90)
    lines.append("")
    
    # Find best and worst performers
    by_mean = sorted(zip(labels, stats_list), key=lambda x: x[1]['mean'])
    by_peak = sorted(zip(labels, stats_list), key=lambda x: x[1]['max'])
    
    lines.append(f"🟢 LOWEST AVG CPU:  {by_mean[0][0]} ({by_mean[0][1]['mean']:.1f}%)")
    lines.append(f"🔴 HIGHEST AVG CPU: {by_mean[-1][0]} ({by_mean[-1][1]['mean']:.1f}%)")
    lines.append("")
    lines.append(f"🟢 LOWEST PEAK CPU:  {by_peak[0][0]} ({by_peak[0][1]['max']:.1f}%)")
    lines.append(f"🔴 HIGHEST PEAK CPU: {by_peak[-1][0]} ({by_peak[-1][1]['max']:.1f}%)")
    lines.append("")
    
    # CPU efficiency comparison
    lines.append("CPU EFFICIENCY:")
    for label, stats in zip(labels, stats_list):
        headroom = 400 - stats['mean']  # Assuming 4 CPU cores = 400%
        lines.append(f"  {label}: {headroom:.1f}% headroom remaining")
    
    lines.append("")
    lines.append("=" * 90)
    
    return '\n'.join(lines)

def main():
    # Test data files
    tests = [
        ('results/production_load/resources_20251115_200213.csv', 'Baseline (1000 RPS, 256MB, 8 shards)'),
        ('results/production_load/resources_20251115_200510.csv', 'High Buffer (1000 RPS, 512MB, 8 shards)'),
        ('results/production_load/resources_20251115_220423.csv', 'Peak Load (1500 RPS, 512MB, 16 shards)'),
    ]
    
    datasets = []
    labels = []
    stats_list = []
    
    print("Loading CPU data from test runs...\n")
    
    for filename, label in tests:
        try:
            data = load_cpu_data(filename)
            if data:
                datasets.append(data)
                labels.append(label)
                stats = calculate_stats(data)
                stats_list.append(stats)
                print(f"✓ Loaded {len(data)} samples from: {label}")
            else:
                print(f"✗ No data in: {label}")
        except FileNotFoundError:
            print(f"✗ File not found: {filename}")
    
    if not datasets:
        print("\nError: No data loaded!")
        return 1
    
    print(f"\n{len(datasets)} test variants loaded successfully!\n")
    
    # Generate ASCII chart
    print(generate_ascii_chart(datasets, labels))
    
    # Generate statistics table
    print(generate_statistics_table(stats_list, labels))
    
    # Generate insights
    print(generate_comparison_insights(stats_list, labels))
    
    return 0

if __name__ == '__main__':
    sys.exit(main())

