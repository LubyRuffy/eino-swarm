工作区里的 app.log 是一份很长的服务日志。请只统计 ERROR 级别的行：每种错误码（code=Exxx）出现了多少次、首次出现的时间戳（按日志原样）。

结果写入 errors.json，格式为 `{"错误码": {"count": 整数, "first_seen": "时间戳"}}`。
