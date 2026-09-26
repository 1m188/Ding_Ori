// 绝不重复注入：连接管理里最要命的一条不变量。
//
// Load() 每次都新建管道实例 + 起一个 Serve 线程。重复注入会让游戏里有两个
// Serve 各自调 Shutdown()，命令随机落到不同实例上，表现是"按了没反应"和
// "开着开着突然全关了"。
//
// 关键：一旦管道存在，就绝不能再走注入分支 —— 哪怕连不上（被占着）也得等。
// 本套件用一个"抢占者"把管道唯一实例占住来构造这个场景。

using System;
using System.Diagnostics;
using System.IO;
using System.IO.Pipes;
using System.Threading;
using OriTrainerDE;

namespace OriTrainerTests
{
    internal static class InjectOnceSuite
    {
        public static void Run()
        {
            Test.Section("绝不重复注入");

            string logPath = Path.Combine(Test.Dir, "once_" + Process.GetCurrentProcess().Id + ".log");

            Test.KillFakes();
            Thread.Sleep(200);
            if (File.Exists(logPath)) File.Delete(logPath);

            Process fake = Test.StartFake(logPath, 0);

            // 等假游戏把管道建好
            string pipeName = "oritrainer_" + fake.Id;
            bool ready = Test.WaitFor(() => Test.ReadAll(logPath).Contains("PIPE_READY"), 5000);
            Test.Check("假游戏管道就绪", ready, "log missing PIPE_READY");

            // 自己抢先占住这个实例（模拟"另一个修改器实例已经连着"）
            NamedPipeClientStream squatter = new NamedPipeClientStream(".", pipeName, PipeDirection.Out);
            squatter.Connect(2000);
            Test.Check("抢占者已连上", squatter.IsConnected, "not connected");

            // 现在启动真实的 PipeClient：它应该看到 Busy，一直 Waiting，绝不进入 Injecting
            PipeClient.Start();

            Test.Check("管道被占 → Waiting（不注入）",
                Test.WaitFor(() => Status.Connection == ConnectionState.Waiting, 3000),
                "actual=" + Status.Connection);

            // 观察期内持续采样。两条都必须在循环里累计，不能等循环结束后再看一眼 ——
            // 只看最后一刻的话，中途若短暂跳到 Injecting 又退回 Waiting 就漏掉了，
            // 而"中途曾经注入过"恰恰是本套件要抓的失败。
            bool everInjecting = false;
            bool everConnected = false;
            Stopwatch sw = Stopwatch.StartNew();
            while (sw.ElapsedMilliseconds < 3000)
            {
                if (Status.Connection == ConnectionState.Injecting) everInjecting = true;
                if (Status.Connection == ConnectionState.Connected) everConnected = true;
                Thread.Sleep(10);
            }
            Test.Check("观察期内从未进入 Injecting", !everInjecting,
                "管道被占着却仍然走进了注入分支");
            Test.Check("观察期内始终不是 Connected", !everConnected,
                "抢占了唯一实例却还是连上了（说明连的不是被占的那个实例）");

            // 释放占用：PipeClient 应立刻接上，且全程没有注入发生
            squatter.Dispose();
            Test.Check("占用释放后自动接上",
                Test.WaitFor(() => Status.Connection == ConnectionState.Connected, 5000),
                "actual=" + Status.Connection);

            PipeClient.Stop();
            Test.Stop(fake);
            Test.KillFakes();
        }
    }
}
