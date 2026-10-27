工作区里的 sales.csv 是一份销售明细（每行的销售额 = units × unit_price）。请统计后写入 report.json，结构如下：

```json
{
  "total_units": 整数,
  "revenue_by_region": {"north": 数字, "south": 数字, "east": 数字, "west": 数字},
  "top_product_by_units": "销量（units 合计）最高的 product"
}
```

销售额保留两位小数。
