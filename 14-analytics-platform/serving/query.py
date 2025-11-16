"""
Analytics Platform Query Service
統一查詢層：合併批處理（ClickHouse）和流處理（Redis）的結果
"""

import redis
from clickhouse_driver import Client
from datetime import datetime, timedelta
from typing import List, Dict, Any, Tuple
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


class AnalyticsService:
    """分析平台查詢服務"""

    def __init__(
        self,
        clickhouse_host: str = 'localhost',
        redis_host: str = 'localhost',
        redis_port: int = 6379
    ):
        self.clickhouse = Client(host=clickhouse_host)
        self.redis = redis.Redis(host=redis_host, port=redis_port, decode_responses=True)
        logger.info("Analytics Service initialized")

    # ========================================================================
    # 時間序列查詢：合併批處理 + 流處理
    # ========================================================================

    def get_order_count_by_minute(
        self,
        start_time: datetime,
        end_time: datetime
    ) -> List[Tuple[datetime, int]]:
        """
        獲取每分鐘訂單數（合併 ClickHouse 和 Redis）

        Args:
            start_time: 開始時間
            end_time: 結束時間

        Returns:
            [(時間, 訂單數), ...]
        """
        # 1. 確定批處理層的最新時間（假設每小時整點更新）
        batch_latest = start_time.replace(minute=0, second=0, microsecond=0)
        while batch_latest < end_time:
            batch_latest += timedelta(hours=1)
        batch_latest -= timedelta(hours=1)  # 回退到上一個整點

        results = []

        # 2. 查詢批處理層（ClickHouse）- 歷史數據
        if start_time < batch_latest:
            logger.info(f"Querying ClickHouse: {start_time} to {batch_latest}")
            query = """
                SELECT
                    toStartOfMinute(created_at) as minute,
                    count() as count
                FROM fact_orders
                WHERE created_at >= %(start)s AND created_at < %(end)s
                GROUP BY minute
                ORDER BY minute
            """

            batch_results = self.clickhouse.execute(
                query,
                {'start': start_time, 'end': min(end_time, batch_latest)}
            )

            results.extend(batch_results)

        # 3. 查詢速度層（Redis）- 實時數據
        if end_time > batch_latest:
            logger.info(f"Querying Redis: {batch_latest} to {end_time}")
            current = batch_latest
            while current < end_time:
                key = f"order_count:{int(current.timestamp())}"
                count = self.redis.get(key)

                if count:
                    results.append((current, int(count)))
                else:
                    # 如果 Redis 沒有數據，回退到 ClickHouse 查詢
                    fallback_query = """
                        SELECT count() as count
                        FROM fact_orders
                        WHERE created_at >= %(start)s AND created_at < %(end)s
                    """
                    fallback_result = self.clickhouse.execute(
                        fallback_query,
                        {
                            'start': current,
                            'end': current + timedelta(minutes=1)
                        }
                    )
                    if fallback_result:
                        results.append((current, fallback_result[0][0]))

                current += timedelta(minutes=1)

        return results

    # ========================================================================
    # 聚合查詢：使用物化視圖加速
    # ========================================================================

    def get_daily_sales_by_category(
        self,
        start_date: datetime,
        end_date: datetime
    ) -> List[Dict[str, Any]]:
        """
        獲取各類目每日銷售額（使用物化視圖）

        Args:
            start_date: 開始日期
            end_date: 結束日期

        Returns:
            [
                {
                    'category': '3C',
                    'order_date': '2025-01-15',
                    'daily_sales': 125000.50,
                    'order_count': 423,
                    'avg_order_value': 295.50
                },
                ...
            ]
        """
        query = """
            SELECT
                category,
                order_date,
                sum(daily_sales) as daily_sales,
                sum(order_count) as order_count,
                sum(daily_sales) / sum(order_count) as avg_order_value
            FROM mv_daily_sales
            WHERE order_date >= %(start_date)s AND order_date <= %(end_date)s
            GROUP BY category, order_date
            ORDER BY order_date DESC, daily_sales DESC
        """

        results = self.clickhouse.execute(
            query,
            {
                'start_date': start_date.date(),
                'end_date': end_date.date()
            }
        )

        return [
            {
                'category': row[0],
                'order_date': row[1].isoformat(),
                'daily_sales': float(row[2]),
                'order_count': row[3],
                'avg_order_value': float(row[4])
            }
            for row in results
        ]

    def get_province_sales_ranking(self, date: datetime) -> List[Dict[str, Any]]:
        """
        獲取各省份銷售額排名

        Args:
            date: 查詢日期

        Returns:
            [
                {
                    'province': '台北',
                    'total_sales': 3500000.00,
                    'order_count': 12456,
                    'avg_order_value': 281.05
                },
                ...
            ]
        """
        query = """
            SELECT
                province,
                sum(amount) as total_sales,
                count() as order_count,
                avg(amount) as avg_order_value
            FROM fact_orders
            WHERE order_date = %(date)s
            GROUP BY province
            ORDER BY total_sales DESC
        """

        results = self.clickhouse.execute(query, {'date': date.date()})

        return [
            {
                'province': row[0],
                'total_sales': float(row[1]),
                'order_count': row[2],
                'avg_order_value': float(row[3])
            }
            for row in results
        ]

    # ========================================================================
    # 複雜分析查詢：關聯多表
    # ========================================================================

    def get_user_purchase_behavior(
        self,
        start_date: datetime,
        end_date: datetime,
        limit: int = 100
    ) -> List[Dict[str, Any]]:
        """
        分析用戶購買行為（按年齡層和類目）

        Args:
            start_date: 開始日期
            end_date: 結束日期
            limit: 返回結果數量限制

        Returns:
            [
                {
                    'age_group': '25-34',
                    'province': '台北',
                    'category': '3C',
                    'purchase_count': 1523,
                    'total_spent': 452190.50,
                    'avg_spent': 297.05
                },
                ...
            ]
        """
        query = """
            SELECT
                CASE
                    WHEN u.age < 25 THEN '18-24'
                    WHEN u.age < 35 THEN '25-34'
                    WHEN u.age < 45 THEN '35-44'
                    WHEN u.age < 55 THEN '45-54'
                    ELSE '55+'
                END as age_group,
                u.province,
                o.category,
                count() as purchase_count,
                sum(o.amount) as total_spent,
                avg(o.amount) as avg_spent
            FROM fact_orders o
            JOIN dim_users u ON o.user_id = u.user_id
            WHERE o.order_date >= %(start_date)s
              AND o.order_date <= %(end_date)s
            GROUP BY age_group, u.province, o.category
            ORDER BY total_spent DESC
            LIMIT %(limit)s
        """

        results = self.clickhouse.execute(
            query,
            {
                'start_date': start_date.date(),
                'end_date': end_date.date(),
                'limit': limit
            }
        )

        return [
            {
                'age_group': row[0],
                'province': row[1],
                'category': row[2],
                'purchase_count': row[3],
                'total_spent': float(row[4]),
                'avg_spent': float(row[5])
            }
            for row in results
        ]

    def get_product_sales_rank(
        self,
        date: datetime,
        limit: int = 10
    ) -> List[Dict[str, Any]]:
        """
        獲取商品銷售排行（使用物化視圖）

        Args:
            date: 查詢日期
            limit: 返回結果數量

        Returns:
            [
                {
                    'product_id': 1001,
                    'product_name': 'iPhone 15 Pro',
                    'category': '3C',
                    'sales_count': 523,
                    'total_revenue': 1569000.00
                },
                ...
            ]
        """
        query = """
            SELECT
                p.product_id,
                p.product_name,
                p.category,
                mv.sales_count,
                mv.total_revenue
            FROM mv_product_sales_rank mv
            JOIN dim_products p ON mv.product_id = p.product_id
            WHERE mv.order_date = %(date)s
            ORDER BY mv.total_revenue DESC
            LIMIT %(limit)s
        """

        results = self.clickhouse.execute(
            query,
            {
                'date': date.date(),
                'limit': limit
            }
        )

        return [
            {
                'product_id': row[0],
                'product_name': row[1],
                'category': row[2],
                'sales_count': row[3],
                'total_revenue': float(row[4])
            }
            for row in results
        ]

    # ========================================================================
    # 實時查詢
    # ========================================================================

    def get_realtime_metrics(self) -> Dict[str, Any]:
        """
        獲取實時指標（過去 5 分鐘）

        Returns:
            {
                'last_5min_orders': 256,
                'last_5min_revenue': 76800.00,
                'avg_order_value': 300.00,
                'orders_per_minute': [
                    {'minute': '2025-01-15T14:25:00', 'count': 52},
                    {'minute': '2025-01-15T14:26:00', 'count': 48},
                    ...
                ]
            }
        """
        now = datetime.now()
        five_min_ago = now - timedelta(minutes=5)

        # 查詢過去 5 分鐘的訂單
        query = """
            SELECT
                toStartOfMinute(created_at) as minute,
                count() as order_count,
                sum(amount) as revenue
            FROM fact_orders
            WHERE created_at >= %(start_time)s
            GROUP BY minute
            ORDER BY minute
        """

        results = self.clickhouse.execute(
            query,
            {'start_time': five_min_ago}
        )

        total_orders = sum(row[1] for row in results)
        total_revenue = sum(float(row[2]) for row in results)

        return {
            'last_5min_orders': total_orders,
            'last_5min_revenue': total_revenue,
            'avg_order_value': total_revenue / total_orders if total_orders > 0 else 0,
            'orders_per_minute': [
                {
                    'minute': row[0].isoformat(),
                    'count': row[1],
                    'revenue': float(row[2])
                }
                for row in results
            ]
        }

    # ========================================================================
    # 漏斗分析
    # ========================================================================

    def get_conversion_funnel(
        self,
        start_date: datetime,
        end_date: datetime
    ) -> Dict[str, Any]:
        """
        獲取轉化漏斗（頁面瀏覽 → 加入購物車 → 購買）

        Returns:
            {
                'page_views': 1000000,
                'add_to_carts': 150000,
                'purchases': 50000,
                'view_to_cart_rate': 15.0,
                'cart_to_purchase_rate': 33.3,
                'overall_conversion_rate': 5.0
            }
        """
        query = """
            SELECT
                countIf(event_type = 'page_view') as page_views,
                countIf(event_type = 'add_to_cart') as add_to_carts,
                countIf(event_type = 'purchase') as purchases
            FROM fact_clickstream
            WHERE toDate(timestamp) >= %(start_date)s
              AND toDate(timestamp) <= %(end_date)s
        """

        result = self.clickhouse.execute(
            query,
            {
                'start_date': start_date.date(),
                'end_date': end_date.date()
            }
        )[0]

        page_views, add_to_carts, purchases = result

        return {
            'page_views': page_views,
            'add_to_carts': add_to_carts,
            'purchases': purchases,
            'view_to_cart_rate': (add_to_carts / page_views * 100) if page_views > 0 else 0,
            'cart_to_purchase_rate': (purchases / add_to_carts * 100) if add_to_carts > 0 else 0,
            'overall_conversion_rate': (purchases / page_views * 100) if page_views > 0 else 0
        }


# ============================================================================
# 使用示例
# ============================================================================

if __name__ == '__main__':
    # 初始化服務
    service = AnalyticsService(
        clickhouse_host='localhost',
        redis_host='localhost'
    )

    # 示例 1：查詢每分鐘訂單數（合併批處理 + 流處理）
    print("\n=== 示例 1：每分鐘訂單數 ===")
    results = service.get_order_count_by_minute(
        start_time=datetime.now() - timedelta(hours=1),
        end_time=datetime.now()
    )
    for minute, count in results[-5:]:  # 顯示最近 5 分鐘
        print(f"{minute}: {count} orders")

    # 示例 2：查詢各類目每日銷售額
    print("\n=== 示例 2：各類目每日銷售額 ===")
    daily_sales = service.get_daily_sales_by_category(
        start_date=datetime.now() - timedelta(days=7),
        end_date=datetime.now()
    )
    for item in daily_sales[:5]:  # 顯示前 5 筆
        print(f"{item['order_date']} - {item['category']}: ${item['daily_sales']:,.2f}")

    # 示例 3：查詢各省份銷售額排名
    print("\n=== 示例 3：各省份銷售額排名 ===")
    province_ranking = service.get_province_sales_ranking(date=datetime.now())
    for i, item in enumerate(province_ranking[:5], 1):
        print(f"{i}. {item['province']}: ${item['total_sales']:,.2f}")

    # 示例 4：實時指標
    print("\n=== 示例 4：實時指標（過去 5 分鐘）===")
    realtime = service.get_realtime_metrics()
    print(f"訂單數: {realtime['last_5min_orders']}")
    print(f"銷售額: ${realtime['last_5min_revenue']:,.2f}")
    print(f"平均客單價: ${realtime['avg_order_value']:,.2f}")

    # 示例 5：轉化漏斗
    print("\n=== 示例 5：轉化漏斗 ===")
    funnel = service.get_conversion_funnel(
        start_date=datetime.now() - timedelta(days=7),
        end_date=datetime.now()
    )
    print(f"頁面瀏覽: {funnel['page_views']:,}")
    print(f"加入購物車: {funnel['add_to_carts']:,} ({funnel['view_to_cart_rate']:.1f}%)")
    print(f"購買: {funnel['purchases']:,} ({funnel['cart_to_purchase_rate']:.1f}%)")
    print(f"整體轉化率: {funnel['overall_conversion_rate']:.1f}%")
