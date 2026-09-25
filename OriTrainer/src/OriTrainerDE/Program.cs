/*
    修改器主程序

    ---- 连接模型：开局连一次，全程持有 ----
    管道在启动时连接一次并一直持有到退出。这是 DLL 侧 Shutdown 语义所要求的：
    Loader.Serve 每次"连上"和"断开"都会停止全部功能，所以"断开"必须只发生在
    修改器真正退出时。反过来说，**一次命令连一次**的用法在这里是不成立的 ——
    那样每条命令刚执行的下一秒就被断开时的 Shutdown 撤销掉。

    ---- 主循环 ----
    每 10ms 一轮：取空按键队列 → 按状态改功能开关 → 重画界面。
    按键判断（哪个键对应哪个功能、HOME 全开全关）都在这里，Keyboard 和 UI 都不参与。
*/

using System;
using System.Diagnostics;
using System.IO.Pipes;
using System.Text;
using System.Threading;
using OriTrainerShared;

namespace OriTrainerDE
{
    internal static class Program
    {
        private const string ProcessName = "oriDE"; // 游戏进程名（不带 .exe）
        private const int LoopMs = 10; // 每轮循环间隔

        private static void Main()
        {
            // ---- 找到游戏进程 ----
            Process[] games = Process.GetProcessesByName(ProcessName);
            if (games.Length > 0) Status.Pid = games[0].Id;

            // ---- 取得命令管道 ----
            NamedPipeClientStream pipe = null;
            if (Status.Pid != 0)
            {
                Status.Injection = InjectionState.Running;
                try
                {
                    pipe = PipeClient.Attach(Status.Pid);
                    Status.Injection = InjectionState.Done;
                }
                catch
                {
                    // 注入失败不退出：界面会显示"未注入"，先让用户看到程序还在跑
                    Status.Injection = InjectionState.None;
                }
            }

            Terminal.Enter(); // 失败就直接抛，不在这里兜：没有控制台就画不了界面
            Keyboard.Start();

            try
            {
                Loop(pipe);
            }
            finally
            {
                // 退出即断开管道，DLL 会借这次断开停止全部功能（见 Loader.Serve）
                pipe?.Dispose();
                Keyboard.Stop();
                Terminal.Leave();
            }
        }

        private static void Loop(NamedPipeClientStream pipe)
        {
            string last = null;

            while (true)
            {
                HandleKeys(pipe);

                // 状态只在按键时变化，所以同一帧不必重复写终端。
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

        private static void HandleKeys(NamedPipeClientStream pipe)
        {
            while (Keyboard.TryRead(out KeyEvent e))
            {
                if (pipe == null) continue; // 没有管道（未找到游戏 / 注入失败），按键无处可发

                if (e.VirtualKey == Keyboard.VkHome)
                {
                    ToggleAll(pipe);
                    continue;
                }

                foreach (Feature f in Status.Features)
                {
                    if (Keyboard.NumPadKey(f.Digit) != e.VirtualKey || f.NeedCtrl != e.Ctrl) continue;

                    Send(pipe, f, !f.On);
                    break;
                }
            }
        }

        // HOME：还有没开的就全部打开，已经全开就全部关闭。
        private static void ToggleAll(NamedPipeClientStream pipe)
        {
            bool allOn = true;
            foreach (Feature f in Status.Features)
                if (!f.On) { allOn = false; break; }

            foreach (Feature f in Status.Features)
                if (allOn ? f.On : !f.On)
                    Send(pipe, f, !allOn);
        }

        // 发一条命令并更新状态。
        //
        // on = true 发 "Name Start"，false 发 "Name Stop"；写成功后才改 f.On，
        // 免得界面显示的开关和实际发出的命令不一致。
        //
        // 管道断开时（游戏重启、游戏退出）写入会抛异常，这里把状态改回"未注入"
        // 并在界面上显示出来 —— 静默吞掉的话，用户会以为是热键失灵或功能本身有问题。
        // 重连逻辑尚未实现（后续再做），所以此后按键都不会生效。
        private static void Send(NamedPipeClientStream pipe, Feature f, bool on)
        {
            byte[] command = Encoding.UTF8.GetBytes(
                (on ? f.StartCommand : f.StopCommand) + Constants.Terminator);

            try
            {
                pipe.Write(command, 0, command.Length);
                pipe.Flush();
                f.On = on;
            }
            catch
            {
                Status.Injection = InjectionState.None;
            }
        }
    }
}
