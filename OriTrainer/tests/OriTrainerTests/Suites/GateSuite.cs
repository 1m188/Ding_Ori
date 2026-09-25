// 就绪门：复现"先开修改器、后开游戏"，断言危险窗口内不注入。
//
// 这条门是为修一个真机崩溃加的 —— 在托管运行时装载完之前注入，
// mono_thread_attach 会解引用未初始化指针，游戏以 0xC0000005 崩溃。
// 详见 PipeClient 文件头。本套件锁住两件事：
//   1. 残留日志（内容含就绪标记、但写入时间早于本次进程启动）不被误判为就绪；
//   2. 门放行之前一次都不尝试注入。
//
// 移植自开发期脚手架，断言逐条保留。

using System;
using System.Diagnostics;
using System.IO;
using System.Reflection;
using System.Threading;
using OriTrainerDE;

namespace OriTrainerTests
{
    internal static class GateSuite
    {
        private static bool Ready()
        {
            MethodInfo m = typeof(PipeClient).GetMethod("RuntimeReady",
                BindingFlags.NonPublic | BindingFlags.Static);
            return (bool)m.Invoke(null, null);
        }

        private static void Tick()
        {
            MethodInfo m = typeof(PipeClient).GetMethod("Tick",
                BindingFlags.NonPublic | BindingFlags.Static);
            m.Invoke(null, null);
        }

        // Injecting 是 Tick() 内部的瞬时状态（注入失败就立刻回 Waiting），
        // 轮询采不到，所以用采样线程死循环读 Status.Connection。
        private static volatile bool _samplerRunning;
        private static volatile int _seenInjecting;
        private static volatile int _seenInjectingBeforeReady;

        private static void SampleLoop()
        {
            while (_samplerRunning)
            {
                if (Status.Connection != ConnectionState.Injecting) continue;

                _seenInjecting++;
                if (!Ready()) _seenInjectingBeforeReady++;
            }
        }

        public static void Run()
        {
            Test.Section("就绪门（先开修改器，后开游戏）");

            string cmdLog = Path.Combine(Test.Dir, "gate_cmd.log");
            string gameLog = Test.LoggedGameLog;
            Directory.CreateDirectory(Path.GetDirectoryName(gameLog));

            Test.KillFakes();
            Thread.Sleep(200);
            if (File.Exists(cmdLog)) File.Delete(cmdLog);

            // ---- 场景 1：残留日志不得被误判 ----
            //
            // 预置"上次运行的残留日志"：内容含就绪标记。
            // 天真做法（只看内容）会立刻误判就绪 —— 本测试要证明门挡住了这一点。
            File.WriteAllText(gameLog,
                "Initialize engine version: 5.3.2f1\n" +
                "Begin MonoManager ReloadAssembly\n" +
                "Completed reload, in 0.593 seconds\n");
            Thread.Sleep(1100); // 让 mtime 明显早于即将启动的进程
            Console.WriteLine("  预置残留日志: 含就绪标记, mtime=" +
                File.GetLastWriteTimeUtc(gameLog).ToString("HH:mm:ss.fff"));

            PipeClient.Start();
            Test.Check("无游戏时状态为 NoGame",
                Test.WaitFor(() => Status.Connection == ConnectionState.NoGame, 3000),
                "actual=" + Status.Connection);

            // noPipe：不建管道，"先连"必然失败，才会走到注入分支 ——
            // 要验证的正是"注入之前门必须已经放行"。
            Process fake = Test.StartLoggedFake(cmdLog, gameLog, 50, 600, true, false);
            Console.WriteLine("  假游戏 pid=" + fake.Id + "（不建管道 → 必须走注入分支）");

            _seenInjecting = 0;
            _seenInjectingBeforeReady = 0;
            _samplerRunning = true;
            Thread sampler = new Thread(SampleLoop) { IsBackground = true };
            sampler.Start();

            long tReady = -1;
            Stopwatch sw = Stopwatch.StartNew();
            while (sw.ElapsedMilliseconds < 20000)
            {
                Tick();
                if (tReady < 0 && Ready())
                {
                    tReady = sw.ElapsedMilliseconds;
                    Console.WriteLine("  RuntimeReady=true @ " + tReady + " ms");
                }
                if (tReady >= 0 && _seenInjecting > 0) break;
                Thread.Sleep(5);
            }
            _samplerRunning = false;
            sampler.Join(500);

            Test.Check("残留标记没有被误判为就绪", _seenInjectingBeforeReady == 0,
                "门在就绪前就放行并尝试注入了 — 把上次运行的残留当成了新日志");
            Test.Check("就绪前从未注入", _seenInjectingBeforeReady == 0, "injected too early");
            Test.Check("就绪后确实尝试了注入（采样线程采到 Injecting）", _seenInjecting > 0,
                "never saw Injecting");
            Test.Check("门放行之前一次都没尝试注入", _seenInjectingBeforeReady == 0,
                "before=" + _seenInjectingBeforeReady + " total=" + _seenInjecting);

            Console.WriteLine("  门放行时刻: " + tReady + " ms（在那之前文件的残留标记会骗过只看内容的判据）");

            PipeClient.Stop();
            Test.Stop(fake);

            // ---- 场景 2：正常用法（游戏早就绪，之后才开修改器）必须立刻放行 ----
            Console.WriteLine();
            Console.WriteLine("  [场景] 游戏已就绪后才开修改器");
            if (File.Exists(cmdLog)) File.Delete(cmdLog);

            // 这次要有管道（模拟已注入过的游戏），验证门不会把正常连接卡住。
            File.WriteAllText(gameLog, "Completed reload, in 0.5 seconds\n");
            File.SetLastWriteTimeUtc(gameLog, DateTime.UtcNow);

            Process fake2 = Test.StartLoggedFake(cmdLog, gameLog, 1, 1, false, false);
            Test.WaitFor(() => Test.ReadAll(cmdLog).Contains("PIPE_READY"), 10000);
            Thread.Sleep(200);

            PipeClient.Start();
            Test.Check("正常用法下门立刻放行", Test.WaitFor(Ready, 3000), "Ready() false");
            Test.Check("正常用法下能连上（没被门卡住）",
                Test.WaitFor(() => Status.Connection == ConnectionState.Connected, 8000),
                "actual=" + Status.Connection);

            PipeClient.Stop();
            Test.Stop(fake2);
            Test.KillFakes();
        }
    }
}
