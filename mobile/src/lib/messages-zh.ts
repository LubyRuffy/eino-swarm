import type { MessageKey } from "./messages-en"

export const zh: Record<MessageKey, string> = {
  "scan.title": "扫码绑定这台 PC",
  "scan.hint":
    "打开 zwai 设置 → 手机，用摄像头扫配对 QR。没有摄像头时粘贴同一条 URI。",
  "scan.camera": "扫描二维码",
  "scan.uri": "配对 URI",
  "scan.paste": "粘贴并绑定",
  "scan.showPaste": "改为粘贴 URI",
  "scan.hidePaste": "收起粘贴",
  "scan.retry": "重试",

  "home.app": "zwai",
  "home.inProgress": "进行中",
  "home.recent": "最近",
  "home.more": "更多",
  "home.unlink": "解除绑定",
  "home.new": "新对话",
  "home.project": "项目",
  "home.defaultProject": "默认",
  "home.start": "开始",
  "home.ask": "等待回答",
  "home.live": "进行中",
  "home.newMessage": "新消息",
  "home.relay": "中继",
  "home.direct": "直连",

  "thread.back": "返回",
  "thread.stop": "停止",
  "thread.send": "发送",
  "thread.followUp": "跟进",
  "thread.steer": "插入",
  "thread.answer": "回答",
  "thread.message": "消息",
  "thread.plan": "规划中",
  "thread.image": "图片",
  "thread.running": "运行中",

  "ask.title": "需要你选一下",
  "ask.submit": "提交",
  "ask.other": "其他",

  "err.reconnect": "连接中断。点重试会再连，不会解除绑定。",
  "locale.en": "EN",
  "locale.zh": "中文",
}
