/*
    修改器主程序

    ---- 连接模型：全程持有一条长连接 ----
    管道由 PipeClient 的后台线程维护：连上后一直持有，断了自动重连，游戏重启自动
    接上新进程。主循环不持有管道对象，只管"要不要发命令"。

    这是 DLL 侧 Shutdown 语义所要求的：Loader.Serve 每次"连上"和"断开"都会停止
    全部功能，所以"断开"必须只发生在修改器真正退出（或游戏进程消失）时。
    反过来说，**一次命令连一次**的用法在这里是不成立的 —— 那样每条命令刚执行的
    下一秒就被断开时的 Shutdown 撤销掉。

    ---- 主循环 ----
    每 10ms 一轮：同步连接状态 → 取空按键队列 → 按状态改功能开关 → 重画界面。
    按键判断（哪个键对应哪个功能、HOME 全开全关）都在这里，Keyboard 和 UI 都不参与。

    这里**没有"启动阶段"**：连接由后台线程持续维护，所以启动时会发生的事运行中
    也随时会发生，全都走同一条路 —— 后台线程负责收敛连接，主循环负责画出来。
*/

using System;
using System.Threading;

namespace OriTrainer
{
    internal static class Program
    {
        private const int LoopMs = 10; // 每轮循环间隔

        // 上一轮是否处于已连接状态，用于识别"断开"这个边沿。
        private static bool _connected;

        private static void Main()
        {
            Sounds.Initialize();    // 启动即加载音效资源，早于任何按键
            Terminal.Enter();       // 先拿输出设备，准备绘制界面：失败就直接抛，此刻什么都还没启动，无需清理。
            Keyboard.Start();       // 监听按键：起后台线程，立即返回
            PipeClient.Start();     // 监听游戏进程准备attach：起后台线程，立即返回

            try
            {
                Loop();
            }
            finally
            {
                // 这里是纯防御：正常退出（关窗口）会被直接终止，不会走到 finally。
                // 那种情况下管道由内核关闭，DLL 侧 ReadFile 失败后照样走
                // DisconnectNamedPipe + Shutdown，功能一样全停。
                PipeClient.Stop();
                Keyboard.Stop();
                Terminal.Leave();
            }
        }

        private static void Loop()
        {
            string last = null;

            while (true)
            {
                Sync();
                HandleKeys();

                // 状态只在按键或连接变化时变动，所以同一帧不必重复写终端。
                // 不比较的话这里是每秒 100 次整屏重写，白白闪烁、白烧 CPU。
                string frame = UI.Build();
                if (frame != last)
                {
                    Terminal.Render(frame);
                    last = frame;
                }

                Thread.Sleep(LoopMs);
            }
        }

        // 跟随 PipeClient 维护的连接状态。
        //
        // 断开的瞬间要把全部开关复位：DLL 侧在断开时已经 Shutdown 停掉了一切，
        // 界面若还显示开着就是骗人。这也是"发送成功但没送达"的唯一纠正手段 ——
        // 那种情况下 Feature.On 已经被置为发送后的值，而游戏里其实没生效。
        private static void Sync()
        {
            bool now = Status.Connection == ConnectionState.Connected;

            if (_connected && !now)
                foreach (Feature f in Status.Features) f.On = false;

            _connected = now;
        }

        private static void HandleKeys()
        {
            while (Keyboard.TryRead(out KeyEvent e))
            {
                if (e.VirtualKey == Keyboard.VkHome)
                {
                    ToggleAll();
                    continue;
                }

                foreach (Feature f in Status.Features)
                {
                    if (Keyboard.NumPadKey(f.Digit) != e.VirtualKey || f.NeedCtrl != e.Ctrl) continue;

                    bool on = !f.On;
                    Send(f, on);
                    if (on) Sounds.PlayOn(); else Sounds.PlayOff(); // 按键动作的反馈
                    break;
                }
            }
        }

        // HOME：还有没开的就全部打开，已经全开就全部关闭。
        private static void ToggleAll()
        {
            bool allOn = true;
            foreach (Feature f in Status.Features)
                if (!f.On) { allOn = false; break; }

            foreach (Feature f in Status.Features)
                if (allOn ? f.On : !f.On)
                    Send(f, !allOn);

            // 只响一声整体反馈，不随每个开关连响。
            if (allOn) Sounds.PlayOff(); else Sounds.PlayOn();
        }

        // 发一条命令并更新状态。
        //
        // on = true 发 "Name Start"，false 发 "Name Stop"；写成功后才改 f.On，
        // 免得界面显示的开关和实际发出的命令不一致。
        //
        // 发送失败（未连接 / 游戏刚退出）什么都不做：按键丢弃，不排队、不在重连后
        // 补发 —— 补发会让用户按了三下、连上后突然自己开三个功能，比丢键更吓人。
        private static void Send(Feature f, bool on)
        {
            if (PipeClient.TrySend(on ? f.StartCommand : f.StopCommand))
                f.On = on;
        }
    }
}
