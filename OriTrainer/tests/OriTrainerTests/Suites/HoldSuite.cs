// 日志被持续持有：就绪门仍须正常工作。
//
// 这是"一直卡在未连接"那次真机故障的根因回归 —— File.ReadAllText 内部是
// FileShare.Read，而共享检查是双向的：游戏全程持有日志的【写】句柄，
// 于是打开必然抛 IOException，被 catch 吞掉后判据永远为假。
// 本套件用假游戏复刻"持有写句柄"，并带一条对照，证明旧写法确实会失败。
//
// ⚠ 那条对照断言是故意保留的：它锁住的是"为什么必须显式 FileShare.ReadWrite"。
// 若有人把读日志改回 File.ReadAllText，这里会红。

using System;
using System.Diagnostics;
using System.IO;
using System.Reflection;
using System.Threading;
using OriTrainerDE;

namespace OriTrainerTests
{
    internal static class HoldSuite
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

        private static volatile bool _samplerRunning;
        private static volatile int _seenInjecting;

        private static void SampleLoop()
        {
            while (_samplerRunning)
                if (Status.Connection == ConnectionState.Injecting) _seenInjecting++;
        }

        public static void Run()
        {
            Test.Section("日志被持续持有时的就绪门");

            string cmdLog = Path.Combine(Test.Dir, "hold_cmd.log");
            string gameLog = Test.LoggedGameLog;
            Directory.CreateDirectory(Path.GetDirectoryName(gameLog));

            Test.KillFakes();
            Thread.Sleep(300);
            if (File.Exists(cmdLog)) File.Delete(cmdLog);

            // 假游戏：清空日志 → 写标记 → 一直持有写句柄 → 不建管道（逼出注入分支）
            Process fake = Test.StartLoggedFake(cmdLog, gameLog, 50, 300, true, true);
            Console.WriteLine("  假游戏 pid=" + fake.Id + "（持有日志写句柄 + 不建管道）");
            Test.WaitFor(() => Test.ReadAll(cmdLog).Contains("HOLDING"), 15000);
            Thread.Sleep(300);

            // _logPath 由 Tick() 在发现进程时填充，所以必须先巡检一次再问 Ready()
            PipeClient.Start();
            Tick();

            // 把门依赖的每个输入都摊开打出来 —— 失败时能直接看出是哪一环
            {
                FieldInfo lf = typeof(PipeClient).GetField("_logPath", BindingFlags.NonPublic | BindingFlags.Static);
                FieldInfo gf = typeof(PipeClient).GetField("_gameStartUtc", BindingFlags.NonPublic | BindingFlags.Static);
                string lp = (string)lf.GetValue(null);
                DateTime gs = (DateTime)gf.GetValue(null);
                Console.WriteLine("  _logPath      = " + (lp ?? "<null>"));
                Console.WriteLine("  _gameStartUtc = " + (gs == DateTime.MinValue ? "<MinValue>" : gs.ToString("HH:mm:ss.fff")));

                if (lp != null && File.Exists(lp))
                {
                    DateTime mt = File.GetLastWriteTimeUtc(lp);
                    Console.WriteLine("  日志 mtime    = " + mt.ToString("HH:mm:ss.fff"));
                    Console.WriteLine("  条件1 (mt>gs) = " + (mt > gs));
                    Console.WriteLine("  条件2 (含标记)= " + Test.ReadAll(lp).Contains("Completed reload"));
                }
            }

            Test.Check("日志被持有时 RuntimeReady 仍为 true", Ready(),
                "Ready()=false —— 读日志被占用挡住了（FileShare 问题）");

            _seenInjecting = 0;
            _samplerRunning = true;
            Thread sampler = new Thread(SampleLoop) { IsBackground = true };
            sampler.Start();

            Stopwatch sw = Stopwatch.StartNew();
            while (sw.ElapsedMilliseconds < 12000)
            {
                Tick();
                if (_seenInjecting > 0) break;
                Thread.Sleep(20);
            }
            _samplerRunning = false;
            sampler.Join(500);

            Test.Check("被持有时仍能走到 Injecting（门放行）", _seenInjecting > 0,
                "Conn=" + Status.Connection + " Ready=" + Ready() + " seenInjecting=" + _seenInjecting);

            PipeClient.Stop();
            Test.Stop(fake);

            // ---- 对照：确认旧写法在有写句柄时确实会失败 ----
            Console.WriteLine();
            Console.WriteLine("  [对照] 证明旧写法（File.ReadAllText）在有写句柄时必然失败");
            Process fake2 = Test.StartLoggedFake(cmdLog, gameLog, 50, 300, true, true);
            Test.WaitFor(() => Test.ReadAll(cmdLog).Contains("HOLDING"), 15000);
            Thread.Sleep(300);

            bool oldFails;
            try { oldFails = !File.ReadAllText(gameLog).Contains("Completed reload"); }
            catch { oldFails = true; }

            Test.Check("File.ReadAllText 在有写句柄时抛异常（即旧 bug）", oldFails,
                "竟然成功了？那说明这次不是这个原因");

            Test.Stop(fake2);
            Test.KillFakes();
        }
    }
}
