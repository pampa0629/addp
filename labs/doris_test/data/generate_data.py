#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Apache Doris 测试数据生成脚本

用法:
    python generate_data.py --type user_events --count 100000 --output user_events.csv
    python generate_data.py --type orders --count 50000 --output orders.csv
    python generate_data.py --type logs --count 1000000 --output logs.csv
"""

import argparse
import csv
import json
import random
from datetime import datetime, timedelta
from typing import List, Dict

try:
    from faker import Faker
    import pandas as pd
except ImportError:
    print("请先安装依赖: pip install faker pandas")
    exit(1)


class DorisDataGenerator:
    """Doris 测试数据生成器"""

    def __init__(self, locale='zh_CN'):
        self.faker = Faker(locale)
        self.start_date = datetime(2025, 1, 1)

    def generate_user_events(self, count: int) -> List[Dict]:
        """生成用户行为数据"""
        events = []
        event_types = ['view', 'click', 'cart', 'purchase']
        devices = ['iOS', 'Android', 'Web', 'Chrome', 'Safari', 'Firefox']
        pages = [
            '/home', '/product/1', '/product/2', '/product/3',
            '/category/electronics', '/category/books', '/category/clothing',
            '/cart', '/checkout', '/my/orders'
        ]

        print(f"生成 {count} 条用户行为数据...")
        for i in range(count):
            event_time = self.start_date + timedelta(
                seconds=random.randint(0, 30 * 24 * 3600)  # 30 天内
            )
            events.append({
                'user_id': random.randint(1000, 9999),
                'event_time': event_time.strftime('%Y-%m-%d %H:%M:%S'),
                'event_type': random.choice(event_types),
                'page_url': random.choice(pages),
                'device': random.choice(devices),
                'duration': random.randint(5, 300)
            })

            if (i + 1) % 10000 == 0:
                print(f"  已生成 {i + 1} 条...")

        return events

    def generate_orders(self, count: int) -> List[Dict]:
        """生成订单数据"""
        orders = []
        categories = ['Electronics', 'Books', 'Clothing', 'Food', 'Sports', 'Home']
        statuses = ['pending', 'paid', 'shipped', 'completed', 'cancelled']

        print(f"生成 {count} 条订单数据...")
        for i in range(count):
            order_time = self.start_date + timedelta(
                seconds=random.randint(0, 30 * 24 * 3600)
            )
            category = random.choice(categories)
            orders.append({
                'order_id': 100000 + i,
                'user_id': random.randint(1000, 9999),
                'product_id': random.randint(1, 1000),
                'category': category,
                'amount': round(random.uniform(10, 10000), 2),
                'quantity': random.randint(1, 10),
                'order_time': order_time.strftime('%Y-%m-%d %H:%M:%S'),
                'status': random.choice(statuses)
            })

            if (i + 1) % 10000 == 0:
                print(f"  已生成 {i + 1} 条...")

        return orders

    def generate_logs(self, count: int) -> List[Dict]:
        """生成日志数据"""
        logs = []
        log_levels = ['INFO', 'WARN', 'ERROR', 'DEBUG']
        services = ['api-service', 'db-service', 'cache-service', 'auth-service', 'worker-service']
        messages = [
            'Request received',
            'Response sent',
            'Connection timeout',
            'Cache miss',
            'Database query executed',
            'Authentication successful',
            'Task completed',
            'Error occurred',
            'Warning: High memory usage',
            'Connection established'
        ]

        print(f"生成 {count} 条日志数据...")
        for i in range(count):
            log_time = self.start_date + timedelta(
                seconds=random.randint(0, 7 * 24 * 3600)  # 7 天内
            )
            logs.append({
                'log_time': log_time.strftime('%Y-%m-%d %H:%M:%S'),
                'log_level': random.choice(log_levels),
                'service': random.choice(services),
                'message': random.choice(messages),
                'ip_address': self.faker.ipv4()
            })

            if (i + 1) % 10000 == 0:
                print(f"  已生成 {i + 1} 条...")

        return logs

    def generate_users(self, count: int) -> List[Dict]:
        """生成用户数据"""
        users = []
        cities = ['Beijing', 'Shanghai', 'Guangzhou', 'Shenzhen', 'Hangzhou', 'Chengdu']

        print(f"生成 {count} 条用户数据...")
        for i in range(count):
            register_date = self.start_date + timedelta(
                days=random.randint(-365, 0)  # 过去一年内
            )
            users.append({
                'user_id': 1000 + i,
                'username': self.faker.user_name(),
                'email': self.faker.email(),
                'phone': self.faker.phone_number(),
                'city': random.choice(cities),
                'register_date': register_date.strftime('%Y-%m-%d'),
                'is_active': random.choice([True, False]),
                'last_login_time': (register_date + timedelta(days=random.randint(0, 30))).strftime('%Y-%m-%d %H:%M:%S')
            })

            if (i + 1) % 10000 == 0:
                print(f"  已生成 {i + 1} 条...")

        return users

    def save_to_csv(self, data: List[Dict], output_file: str):
        """保存为 CSV 文件"""
        if not data:
            print("没有数据可保存")
            return

        print(f"保存数据到 {output_file}...")
        df = pd.DataFrame(data)
        df.to_csv(output_file, index=False, encoding='utf-8')
        print(f"✅ 成功保存 {len(data)} 条数据到 {output_file}")
        print(f"   文件大小: {self._get_file_size(output_file)}")

    def save_to_json(self, data: List[Dict], output_file: str):
        """保存为 JSON 文件"""
        if not data:
            print("没有数据可保存")
            return

        print(f"保存数据到 {output_file}...")
        with open(output_file, 'w', encoding='utf-8') as f:
            json.dump(data, f, ensure_ascii=False, indent=2)
        print(f"✅ 成功保存 {len(data)} 条数据到 {output_file}")
        print(f"   文件大小: {self._get_file_size(output_file)}")

    def _get_file_size(self, file_path: str) -> str:
        """获取文件大小（格式化）"""
        import os
        size = os.path.getsize(file_path)
        for unit in ['B', 'KB', 'MB', 'GB']:
            if size < 1024.0:
                return f"{size:.2f} {unit}"
            size /= 1024.0
        return f"{size:.2f} TB"


def main():
    parser = argparse.ArgumentParser(description='Doris 测试数据生成器')
    parser.add_argument('--type', required=True,
                        choices=['user_events', 'orders', 'logs', 'users'],
                        help='数据类型')
    parser.add_argument('--count', type=int, default=10000,
                        help='生成数据条数（默认: 10000）')
    parser.add_argument('--output', required=True,
                        help='输出文件路径')
    parser.add_argument('--format', default='csv',
                        choices=['csv', 'json'],
                        help='输出格式（默认: csv）')

    args = parser.parse_args()

    generator = DorisDataGenerator()

    # 生成数据
    if args.type == 'user_events':
        data = generator.generate_user_events(args.count)
    elif args.type == 'orders':
        data = generator.generate_orders(args.count)
    elif args.type == 'logs':
        data = generator.generate_logs(args.count)
    elif args.type == 'users':
        data = generator.generate_users(args.count)
    else:
        print(f"不支持的数据类型: {args.type}")
        return

    # 保存数据
    if args.format == 'csv':
        generator.save_to_csv(data, args.output)
    else:
        generator.save_to_json(data, args.output)

    print("\n📊 数据统计:")
    print(f"   数据类型: {args.type}")
    print(f"   数据条数: {len(data)}")
    print(f"   输出格式: {args.format.upper()}")
    print(f"   输出文件: {args.output}")

    if data:
        print("\n📝 数据示例（前 3 条）:")
        for i, record in enumerate(data[:3]):
            print(f"   {i + 1}. {record}")


if __name__ == '__main__':
    main()
