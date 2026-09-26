// 就绪门：复现"先开修改器、后开游戏"，断言危险窗口内不注入。
//
// 这条门是为修一个真机崩溃加的 —— 在托管运行时装载完之前注入，
// mono_thread_attach 会解引用未初始化指针，游戏以 0xC0000005 崩溃。
// 详见 PipeClient 文件头。本套件锁住两件事：
//   1. 残留日志（内容含就绪标记、但写入时间早于本次进程启动）不被误判为就绪；
//   2. 门放行之前一次都不尝试注入。
//
// ---- 并发约定（改本文件前先看这段）----
// 本套件的 Tick() 全部由主线程调用，不起 PipeClient 的巡检线程。原因是
// Tick() 会写 _logPath / _gameStartUtc / _injectedPid 与 Status，而 Ready() 读的
// 正是前两个；两个线程各调一边就是测试自己制造数据竞争。
//   · 采样线程只读 Status.Connection（后台写、别处读本就是它的设计用法）；
//   · 需要 Ready() 时由主线程在 Tick() 之后调用，中间没有别的写入者；
//   · 场景 2 的最后一步必须改用 PipeClient.Start()，因为保存新管道前会检查
//     _running，Stop() 之后手动 Tick() 永远连不上（原因写在那一处）。
//
// ---- 关于"门确实关着"是怎么保证的 ----
// 不靠读门的状态来自证，而是靠【假游戏自己的时钟】：readyMs 之前它绝不会写下
// 就绪标记，所以那段时间门必然为假。于是"危险窗口内没注入"这条断言是确定性的，
// 不依赖任何跨线程读取。

using System;
using System.Diagnostics;
using System.IO;
using System.Reflection;
using System.Threading;
using OriTrainer;

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
        // 单靠调用方在 Tick() 返回后去看是采不到的，所以用一个采样线程盯着。
        //
        // ⚠ 采样线程【只读 Status.Connection】，绝不调用 Ready()。
        //   Status.Connection 是 4 字节 enum，读写原子，而且"后台线程写、别处读"
        //   本来就是它被设计成的用法（UI 就是这么读的）。
        //   若让采样线程也调 Ready()，它会与 Tick() 并发读写 _logPath / _gameStartUtc
        //   （都非 volatile、无同步，且 DateTime 是 8 字节、读取可能被撕裂），
        //   那是测试自己制造的竞争，会把失败归因引到错误的方向。
        //
        // 计数【封顶】：这个循环没有 sleep，一旦门坏掉、状态长时间停在 Injecting，
        // 自增会在几秒内跑过 int.MaxValue 回绕成负数，于是"采到过 Injecting"变成假、
        // 断言反而看起来是另一个毛病。我们只关心"0 还是非 0"，封顶即可。
        private const int SeenCap = 1000000;

        private static volatile bool _samplerRunning;
        private static volatile int _seenInjecting;

        private static void SampleLoop()
        {
            while (_samplerRunning)
                if (Status.Connection == ConnectionState.Injecting && _seenInjecting < SeenCap)
                    _seenInjecting++;
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

            // 本套件全程由本线程调用 Tick()，不起 PipeClient 的巡检线程：
            // 两个线程并发跑 Tick() 会同时改 _injectedPid / _logPath / _gameStartUtc 与
            // Status，状态迁移就不再可预期，"某一刻是否已就绪"也无法断定。
            // Stop() 是把前一个套件可能留下的巡检线程停掉，确保下面只有本线程在动。
            PipeClient.Stop();
            Thread.Sleep(300); // 给可能残留的巡检线程时间退出循环

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

            Tick();
            Test.Check("无游戏时状态为 NoGame", Status.Connection == ConnectionState.NoGame,
                "actual=" + Status.Connection);

            // noPipe：不建管道，"先连"必然失败，才会走到注入分支 ——
            // 要验证的正是"注入之前门必须已经放行"。
            //
            // 就绪时刻给到 2500ms（比默认的 600ms 长得多），是为了让"门确实关着"这件事
            // 由【假游戏自己的时钟】保证，而不是靠采样线程去读门的状态来判断：
            // 假游戏在 readyMs 之前绝不会写下就绪标记，所以这段时间内门必然为假。
            // 于是"危险窗口内一次都没注入"这条断言不依赖任何跨线程读取，是确定性的。
            const int ReadyMs = 2500;
            const int ShutWindowMs = 2000; // < ReadyMs，留足余量
            Process fake = Test.StartLoggedFake(cmdLog, gameLog, 50, ReadyMs, true, false);
            Console.WriteLine("  假游戏 pid=" + fake.Id + "（不建管道 → 必须走注入分支）");

            _seenInjecting = 0;
            _samplerRunning = true;
            Thread sampler = new Thread(SampleLoop) { IsBackground = true };
            sampler.Start();

            long tReady = -1;
            int seenAtShutEnd = -1;
            Stopwatch sw = Stopwatch.StartNew();
            while (sw.ElapsedMilliseconds < 20000)
            {
                Tick();

                // 每轮都问 Ready()（主线程调用，安全）。不要挪到下面的分支里去 ——
                // 那样 tReady 只可能在窗口结束之后才被赋值，于是"门放行是否早于标记"
                // 那条断言会恒真、永远抓不到门坏掉的情况（门坏了恰恰就是 tReady≈0）。
                if (tReady < 0 && Ready())
                {
                    tReady = sw.ElapsedMilliseconds;
                    Console.WriteLine("  RuntimeReady=true @ " + tReady + " ms");
                }

                // 窗口内：门必然关着（假游戏还没写标记），此时绝不该有注入
                if (sw.ElapsedMilliseconds <= ShutWindowMs) seenAtShutEnd = _seenInjecting;

                if (tReady >= 0 && _seenInjecting > 0) break;
                Thread.Sleep(5);
            }
            _samplerRunning = false;
            sampler.Join(500);

            Console.WriteLine("  门放行时刻: " + tReady + " ms（在那之前文件的残留标记会骗过只看内容的判据）");
            Console.WriteLine("  关门窗口 " + ShutWindowMs + "ms 结束时的注入次数: " + seenAtShutEnd);

            // 这条不依赖并发读取：seenAtShutEnd 是主循环在 Tick() 之后、且仍在
            // readyMs 之前读到的计数；那段时间内假游戏尚未写标记，门必然为假。
            Test.Check("门关着的窗口内一次都没尝试注入", seenAtShutEnd == 0,
                "在 " + ShutWindowMs + "ms 前就注入 " + seenAtShutEnd + " 次 —— 残留日志被骗过了");
            Test.Check("就绪后确实尝试了注入（采样线程采到 Injecting）", _seenInjecting > 0,
                "never saw Injecting");
            // 容差 500ms：sw 从 Process.Start 返回时开始计时，而假游戏是从它自己的 Main
            // 开始计时的，两者之间有一个进程启动的偏移，方向不定。门若真的坏了会在
            // ~0ms 就放行，所以这个容差既够宽容也不会放过真故障。
            Test.Check("门放行发生在假游戏写下标记之后", tReady >= ReadyMs - 500,
                "tReady=" + tReady + " 早于 readyMs=" + ReadyMs);

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

            // 先用本线程 Tick() 一次，把 _logPath / _gameStartUtc 填好 —— Ready() 读的
            // 正是这两个字段。此时巡检线程还没起，主线程直接问 Ready() 是安全的。
            Tick();
            Test.Check("正常用法下门立刻放行", Ready(), "Ready() false");

            // 再交给巡检线程去完成连接：只读 Status.Connection（它本来就设计成
            // 后台线程写、别处读），不再在主线程并发调用 Tick()/Ready()。
            //
            // ⚠ 这里必须走 Start() 而不能继续手动 Tick()：TryConnect 在挂上新管道前会检查
            // _running，Stop() 之后它为 false，于是连接成功也不保存、直接返回 Busy，
            // 状态会永远停在 Waiting。手动 Tick() 只适用于"验证注入路径"（Gate 场景 1
            // 与 Hold），因为那条路径不依赖 _running。
            PipeClient.Start();
            Test.Check("正常用法下能连上（没被门卡住）",
                Test.WaitFor(() => Status.Connection == ConnectionState.Connected, 8000),
                "actual=" + Status.Connection);

            PipeClient.Stop();
            Test.Stop(fake2);
            Test.KillFakes();
        }
    }
}
