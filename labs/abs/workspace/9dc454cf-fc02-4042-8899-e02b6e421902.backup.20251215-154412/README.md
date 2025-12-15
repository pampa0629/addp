项目说明

- 后端使用 Python 标准库启动本地服务，首次启动会在 `data/data.csv` 创建 1-10 月份的随机产值（1-10 之间的整数）。
- 前端是纯静态页面，访问后会调用 `/api/data` 接口获取 CSV 中的数据并用 Canvas 绘制柱状图。

快速开始

1) 启动服务

```
python3 server.py
```

2) 打开浏览器访问

```
http://localhost:8000
```

页面上可以点击「随机生成新数据」按钮，后端会重新随机生成 CSV，并即时刷新柱状图。

文件结构

- `server.py`：后端服务与 CSV 生成/读取，以及接口 `/api/data`、`/api/regenerate`。
- `public/index.html`：前端页面与 Canvas 柱状图。
- `data/data.csv`：生成的 CSV 数据文件（运行后出现）。


