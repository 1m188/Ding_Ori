// 各套件共用的基础设施：断言计数、等待、假游戏启停、路径。
//
// 所有套件都通过 Test 静态类打交道，于是"怎么算通过""假游戏在哪"
// 这类问题只有一处答案，新加套件不必再抄一遍。

using System;
using System.Diagnostics;
using System.IO;
using System.Threading;

namespace OriTrainerTests
{
    internal static class Test
    {
        private static int _pass;
        private static int _fail;

        public static int Pass { get { return _pass; } }
        public static int Fail { get { return _fail; } }

        // ---- 测试输出目录 ----
        // 假游戏、Grab、以及本项目都落在同一个目录（由 csproj 的 StageTestHelpers 保证）。
        public static string Dir
        {
            get { return Path.GetDirectoryName(typeof(Test).Assembly.Location); }
        }

        // 仅管道模式的假游戏（放在输出根目录）
        public static string FakeExe
        {
            get { return Path.Combine(Dir, "oriDE.exe"); }
        }

        // 日志模式的假游戏（放在 FakeGame\ 子目录，日志随之落在 FakeGame\OriDE_Data\）
        public static string LoggedFakeExe
        {
            get { return Path.Combine(Dir, "FakeGame", "oriDE.exe"); }
        }

        public static string LoggedGameLog
        {
            get { return Path.Combine(Dir, "FakeGame", "OriDE_Data", "output_log.txt"); }
        }

        public static string GrabExe
        {
            get { return Path.Combine(Dir, "Grab.exe"); }
        }

        // 被测的真实修改器 exe。
        //
        // 不能靠"从输出目录往上数几层"来拼路径 —— 层数会随 TFM/RID 变化而失效
        // （踩过一次：数错一层，指向了 tests\OriTrainerTests\src\... 这个不存在的位置）。
        // 改为直接写死绝对的仓库根，并且【先验证它真的存在】，不存在就明确报出来，
        // 而不是让 E2E/Soak 拿着一个坏路径去启动进程、报一个看不懂的错。
        public static string TrainerExe
        {
            get
            {
                return Path.GetFullPath(Path.Combine(RepoRoot,
                    @"src\OriTrainerDE\bin\Release\net48\win-x86\OriTrainerDE.exe"));
            }
        }

        // 仓库根：从本程序集位置向上找到含 OriTrainer.sln 的那一层。
        // 这样无论输出路径怎么变，定位方式都成立。
        public static string RepoRoot
        {
            get
            {
                DirectoryInfo d = new DirectoryInfo(Dir);
                while (d != null && !File.Exists(Path.Combine(d.FullName, "OriTrainer.sln")))
                    d = d.Parent;

                if (d == null)
                    throw new Exception("找不到仓库根（向上找不到 OriTrainer.sln），起始于 " + Dir);

                return d.FullName;
            }
        }

        // ---- 断言 ----
        public static void Check(string what, bool ok, string detail)
        {
            if (ok) { _pass++; Console.WriteLine("  PASS  " + what); }
            else { _fail++; Console.WriteLine("  FAIL  " + what + "   <-- " + detail); }
        }

        public static void Section(string title)
        {
            Console.WriteLine();
            Console.WriteLine("== " + title + " ==");
        }

        public static int Report()
        {
            Console.WriteLine();
            Console.WriteLine("PASS=" + _pass + "  FAIL=" + _fail);
            return _fail == 0 ? 0 : 1;
        }

        // ---- 等待 ----
        public static bool WaitFor(Func<bool> cond, int ms)
        {
            Stopwatch sw = Stopwatch.StartNew();
            while (sw.ElapsedMilliseconds < ms)
            {
                if (cond()) return true;
                Thread.Sleep(20);
            }
            return cond();
        }

        // ---- 假游戏 ----

        // 杀掉所有残留假游戏。每个套件开头都要做，否则上一轮没清干净的 oriDE
        // 会让 PipeClient 立刻"发现游戏"，断言全乱。
        public static void KillFakes()
        {
            foreach (Process p in Process.GetProcessesByName("oriDE"))
            {
                try { p.Kill(); p.WaitForExit(2000); } catch { }
                p.Dispose();
            }
        }

        // 启动仅管道模式的假游戏。args 里可给建管道延迟。
        public static Process StartFake(string cmdLog, int pipeDelayMs)
        {
            ProcessStartInfo psi = new ProcessStartInfo(FakeExe,
                "\"" + cmdLog + "\" " + pipeDelayMs);
            psi.UseShellExecute = false;
            psi.CreateNoWindow = true;
            return Process.Start(psi);
        }

        // 启动日志模式的假游戏（就绪门相关套件用）。
        public static Process StartLoggedFake(string cmdLog, string gameLog,
            int truncateMs, int readyMs, bool noPipe, bool hold)
        {
            string a = "\"" + cmdLog + "\" \"" + gameLog + "\" " + truncateMs + " " + readyMs;
            if (noPipe) a += " noPipe";
            if (hold) a += " hold";

            ProcessStartInfo psi = new ProcessStartInfo(LoggedFakeExe, a);
            psi.UseShellExecute = false;
            psi.CreateNoWindow = true;
            return Process.Start(psi);
        }

        public static void Stop(Process p)
        {
            if (p == null) return;
            try { p.Kill(); p.WaitForExit(3000); } catch { }
            p.Dispose();
        }

        // ---- 日志读取 ----
        // 必须用 FileShare.ReadWrite：假游戏与真游戏一样，运行期间一直持有写句柄，
        // File.ReadAllText（FileShare.Read）会抛 IOException。本项目的 HoldTest
        // 专门锁住这条 —— 别再改回 File.ReadAllText。
        public static string ReadAll(string path)
        {
            if (!File.Exists(path)) return "";
            for (int i = 0; i < 50; i++)
            {
                try
                {
                    using (FileStream fs = new FileStream(path, FileMode.Open,
                               FileAccess.Read, FileShare.ReadWrite | FileShare.Delete))
                    using (StreamReader r = new StreamReader(fs))
                        return r.ReadToEnd();
                }
                catch { Thread.Sleep(20); }
            }
            return "";
        }

        public static void Ensure(Predicate<string> condition, string path, int ms)
        {
            WaitFor(() => condition(ReadAll(path)), ms);
        }
    }
}
