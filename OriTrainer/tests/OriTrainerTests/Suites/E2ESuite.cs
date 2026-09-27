// 端到端：跑【真实构建出来的修改器 exe】，用 SendInput 模拟热键，
// 借 Grab.exe 子进程截图核对界面与开关复位。
//
// 为什么不用编进来的主循环：
//   Program.cs 有 Main，编进来会和测试入口冲突；更重要的是，
//   这里要验证的正是"真实 exe 在真控制台里的行为"——编进来就测不到
//   Terminal/资源嵌入/清单这些东西。所以本套件启动的是构建产物本身。
//
// 断言核心是"断开时主循环把全部开关复位"—— 这条只能靠真实主循环验证。
//
// 版本差异（用 #if DE 区分）：
//   终极版 14 项功能（普通 1-9 含无限冲刺 7、特殊 Ctrl+1-5 含重置时间 Ctrl+4）；
//   原版 13 项（少无限冲刺，键位紧凑前移：无限能力点数 7、显示地图 8、
//   特殊 Ctrl+1-5）。按键序列与命令计数因此两套。

using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.Runtime.InteropServices;
using System.Text;
using System.Threading;

namespace OriTrainerTests
{
    internal static class E2ESuite
    {
        // ---- 发按键 ----
        [StructLayout(LayoutKind.Sequential)]
        private struct MOUSEINPUT { public int dx, dy; public uint mouseData, dwFlags, time; public IntPtr dwExtraInfo; }
        [StructLayout(LayoutKind.Sequential)]
        private struct KEYBDINPUT { public ushort wVk, wScan; public uint dwFlags, time; public IntPtr dwExtraInfo; }
        [StructLayout(LayoutKind.Sequential)]
        private struct HARDWAREINPUT { public uint uMsg; public ushort wParamL, wParamH; }
        [StructLayout(LayoutKind.Explicit)]
        private struct INPUTUNION
        {
            [FieldOffset(0)] public MOUSEINPUT mi;
            [FieldOffset(0)] public KEYBDINPUT ki;
            [FieldOffset(0)] public HARDWAREINPUT hi;
        }
        [StructLayout(LayoutKind.Sequential)]
        private struct INPUT { public uint type; public INPUTUNION u; }

        [DllImport("user32.dll", SetLastError = true)]
        private static extern uint SendInput(uint n, INPUT[] inputs, int size);

        private const uint INPUT_KEYBOARD = 1, KEYEVENTF_KEYUP = 0x0002;
        private const ushort VK_CONTROL = 0x11;

        // 三个 union 成员都要写全、且按 MOUSEINPUT 定尺寸：
        // 少写会让 SendInput 因结构尺寸不符而静默什么都不注入（踩过）。
        private static INPUT Key(ushort vk, bool up)
        {
            INPUT i = new INPUT();
            i.type = INPUT_KEYBOARD;
            i.u.ki.wVk = vk;
            i.u.ki.dwFlags = up ? KEYEVENTF_KEYUP : 0;
            return i;
        }

        private static void Tap(ushort vk, bool ctrl)
        {
            if (ctrl)
            {
                SendInput(1, new[] { Key(VK_CONTROL, false) }, Marshal.SizeOf(typeof(INPUT)));
                Thread.Sleep(40);
            }
            SendInput(1, new[] { Key(vk, false) }, Marshal.SizeOf(typeof(INPUT)));
            Thread.Sleep(50);
            SendInput(1, new[] { Key(vk, true) }, Marshal.SizeOf(typeof(INPUT)));
            if (ctrl)
            {
                Thread.Sleep(40);
                SendInput(1, new[] { Key(VK_CONTROL, true) }, Marshal.SizeOf(typeof(INPUT)));
            }
            Thread.Sleep(150);
        }

        // ---- 截图 ----
        private static string _logPath;
        private static int _grabSeq;

        // 通过子进程截图：本进程不碰自己的控制台。
        // 子进程把结果以 UTF-8 写文件，这里按 UTF-8 读回（走 stdout 会被代码页毁掉中文）。
        //
        // ⚠ 必须先删掉同名旧文件再启动 Grab。_grabSeq 每个进程从 0 重新开始，所以
        //   screen_0.txt 之类的名字【上一轮跑测试时就已经存在】；若这次 Grab 启动失败
        //   或没来得及写，File.Exists 仍为真，于是会把上一轮的界面当成本轮的读进来，
        //   断言就在对着陈旧的截图判定。删掉之后 File.Exists 只是"这次真的写出来了"。
        private static string[] Screen(int pid)
        {
            string tmp = Path.Combine(Test.Dir, "screen_" + (_grabSeq++) + ".txt");
            try { if (File.Exists(tmp)) File.Delete(tmp); } catch { }

            ProcessStartInfo psi = new ProcessStartInfo(Test.GrabExe, pid + " 80 \"" + tmp + "\"");
            psi.UseShellExecute = false;
            psi.CreateNoWindow = true;

            bool exited;
            using (Process p = Process.Start(psi)) exited = p.WaitForExit(5000);

            // 超时说明 Grab 卡住了：把残留进程杀掉，别让它迟一步写出文件、留在输出目录里。
            if (!exited)
            {
                Process[] grabbies = Process.GetProcessesByName("Grab");
                foreach (Process g in grabbies)
                {
                    try { g.Kill(); g.WaitForExit(1000); } catch { }
                    g.Dispose();
                }
                return new[] { "<grab timeout>" };
            }
            if (!File.Exists(tmp)) return new[] { "<no output>" };

            try
            {
                return File.ReadAllText(tmp, Encoding.UTF8).Replace("\r\n", "\n").Split('\n');
            }
            finally
            {
                // 别把截图留在输出目录里越堆越多（每轮跑 E2E 会调用几十次）。
                try { File.Delete(tmp); } catch { }
            }
        }

        private static int Count(string[] screen, string mark)
        {
            int n = 0;
            foreach (string l in screen) if (l.Contains(mark)) n++;
            return n;
        }

        private static bool Has(string[] screen, string s)
        {
            foreach (string l in screen) if (l.Contains(s)) return true;
            return false;
        }

        private static string Dump(string[] s)
        {
            if (s == null) return "<null>";
            StringBuilder b = new StringBuilder();
            foreach (string l in s) if (!string.IsNullOrEmpty(l)) b.Append("[").Append(l).Append("] ");
            return b.ToString();
        }

        private static List<string> CmdLines()
        {
            List<string> cmds = new List<string>();
            foreach (string l in Test.ReadAll(_logPath).Split('\n'))
            {
                int i = l.IndexOf("CMD ", StringComparison.Ordinal);
                if (i >= 0) cmds.Add(l.Substring(i + 4).Trim());
            }
            return cmds;
        }

        private static int CountCmds() { return CmdLines().Count; }

        private static bool CmdsAre(params string[] expected)
        {
            List<string> got = CmdLines();
            if (got.Count != expected.Length) return false;
            for (int i = 0; i < expected.Length; i++)
                if (got[i] != expected[i]) return false;
            return true;
        }

        public static void Run()
        {
            Test.Section("端到端（真实 exe + 真控制台）");

            string exe = Test.TrainerExe;
            Test.Check("真实 exe 已构建", File.Exists(exe), exe + "（先构建解决方案）");
            if (!File.Exists(exe)) return;

            _logPath = Path.Combine(Test.Dir, "e2e_" + Process.GetCurrentProcess().Id + ".log");

            Test.KillFakes();
            Thread.Sleep(200);
            if (File.Exists(_logPath)) File.Delete(_logPath);

            Process fake = Test.StartFake(_logPath, 0);
            Test.WaitFor(() => Test.ReadAll(_logPath).Contains("PIPE_READY"), 5000);

            // 必须让修改器拿到【自己的】控制台：若继承本进程被重定向的 stdout，
            // GetConsoleMode 会失败 -> Terminal.Enter() 直接抛 -> 修改器当场退出。
            // UseShellExecute=true 等价于双击启动，会为控制台程序新开一个窗口。
            ProcessStartInfo tpsi = new ProcessStartInfo(exe);
            tpsi.UseShellExecute = true;
            Process trainer = Process.Start(tpsi);

            Test.Check("修改器已启动",
                Test.WaitFor(() => { try { trainer.Refresh(); return !trainer.HasExited; } catch { return false; } }, 3000),
                "exited");

            string[] screen = Screen(trainer.Id);
            bool connected = Test.WaitFor(() =>
            {
                screen = Screen(trainer.Id);
                return Has(screen, "已连接");
            }, 10000);
            Test.Check("界面显示已连接", connected, "screen=" + Dump(screen));

#if DE
            const int Total = 14; // 终极版功能总数
#else
            const int Total = 13; // 原版功能总数
#endif
            Test.Check("初始 " + Total + " 个开关全关", Count(screen, "[ ]") == Total && Count(screen, "[x]") == 0,
                "off=" + Count(screen, "[ ]") + " on=" + Count(screen, "[x]") + " screen=" + Dump(screen));

            // ---- 3 个普通热键 ----
            // DE：小键盘 1/3/9（9=显示地图）；原版：小键盘 1/3/8（显示地图紧凑前移到 8）。
            Tap(0x61, false);
            Tap(0x63, false);
#if DE
            Tap(0x69, false); // 小键盘 9 = 显示地图（终极版）
#else
            Tap(0x68, false); // 小键盘 8 = 显示地图（原版）
#endif

            Test.Check("按 3 个热键后界面 3 个开启",
                Test.WaitFor(() => { screen = Screen(trainer.Id); return Count(screen, "[x]") == 3; }, 4000),
                "on=" + Count(screen, "[x]") + " screen=" + Dump(screen));
            Test.Check("假游戏恰好收到 3 条 Start", Test.WaitFor(() => CountCmds() == 3, 3000),
                "count=" + CountCmds());
            Test.Check("命令内容与顺序正确",
                CmdsAre("UnlimitedLife Start", "SoulFlameNoCooldown Start", "ShowMap Start"),
                "cmds=" + string.Join(" | ", CmdLines().ToArray()));

            // ---- Ctrl+组合键：DE 用 Ctrl+小键盘 4（重置时间），原版用 Ctrl+小键盘 5（三把钥匙）----
            // DE 特殊功能：Ctrl+1 死亡数归零、Ctrl+2 100% 探索、Ctrl+3 解锁全部基础技能、
            //              Ctrl+4 重置时间、Ctrl+5 获得三把钥匙。
            // 原版特殊功能：Ctrl+1 死亡数归零、Ctrl+2 100% 探索、Ctrl+3 解锁全部基础技能、
            //              Ctrl+4 重置时间、Ctrl+5 获得三把钥匙（与 DE 相同，仅普通功能少一项）。
#if DE
            Tap(0x64, true); // Ctrl+小键盘 4 = 重置时间
#else
            Tap(0x65, true); // Ctrl+小键盘 5 = 三把钥匙
#endif
            Test.Check("Ctrl 组合键开启第 4 项",
                Test.WaitFor(() => { screen = Screen(trainer.Id); return Count(screen, "[x]") == 4; }, 4000),
                "on=" + Count(screen, "[x]") + " screen=" + Dump(screen));
#if DE
            string fourth = "ResetTime Start";
#else
            string fourth = "GrantKeys Start";
#endif
            Test.Check("Ctrl 组合键命令正确",
                Test.WaitFor(() => CountCmds() == 4, 3000) &&
                CmdsAre("UnlimitedLife Start", "SoulFlameNoCooldown Start", "ShowMap Start", fourth),
                "cmds=" + string.Join(" | ", CmdLines().ToArray()));

            // ---- 再按一次同一键 = 关闭 ----
            Tap(0x61, false);
            Test.Check("再按小键盘 1 关闭该功能",
                Test.WaitFor(() => { screen = Screen(trainer.Id); return Count(screen, "[x]") == 3; }, 4000),
                "on=" + Count(screen, "[x]"));
            Test.Check("关闭发的是 Stop",
                Test.WaitFor(() => CountCmds() == 5, 3000) && CmdLines()[4] == "UnlimitedLife Stop",
                "cmds=" + string.Join(" | ", CmdLines().ToArray()));

            // ---- HOME：还有没开的 → 全部打开 ----
            Tap(0x24, false);
            Test.Check("HOME 全部开启 " + Total + " 个",
                Test.WaitFor(() => { screen = Screen(trainer.Id); return Count(screen, "[x]") == Total; }, 5000),
                "on=" + Count(screen, "[x]") + " screen=" + Dump(screen));
#if DE
            // 原本开着 3 个（SoulFlameNoCooldown/ShowMap/ResetTime），所以只补发 11 条
            Test.Check("HOME 只对关着的 11 项发 Start", Test.WaitFor(() => CountCmds() == 16, 4000),
                "count=" + CountCmds() + " cmds=" + string.Join(" | ", CmdLines().ToArray()));
#else
            // 原本开着 3 个（SoulFlameNoCooldown/ShowMap/GrantKeys），所以只补发 10 条
            Test.Check("HOME 只对关着的 10 项发 Start", Test.WaitFor(() => CountCmds() == 15, 4000),
                "count=" + CountCmds() + " cmds=" + string.Join(" | ", CmdLines().ToArray()));
#endif

            // ---- HOME 再来一次：已全开 → 全部关闭 ----
            Tap(0x24, false);
            Test.Check("再按 HOME 全部关闭",
                Test.WaitFor(() => { screen = Screen(trainer.Id); return Count(screen, "[x]") == 0 && Count(screen, "[ ]") == Total; }, 5000),
                "on=" + Count(screen, "[x]") + " screen=" + Dump(screen));
#if DE
            Test.Check("HOME 关闭发了 14 条 Stop", Test.WaitFor(() => CountCmds() == 30, 4000),
                "count=" + CountCmds());
#else
            Test.Check("HOME 关闭发了 13 条 Stop", Test.WaitFor(() => CountCmds() == 28, 4000),
                "count=" + CountCmds());
#endif

            // ---- 杀游戏：界面回到未检测到 + 开关全复位 ----
            // 先开几个，确认复位真的发生了（而不是本来就没开）
            Tap(0x61, false);
            Tap(0x62, false);
#if DE
            Test.WaitFor(() => CountCmds() == 32, 3000);
#else
            Test.WaitFor(() => CountCmds() == 30, 3000);
#endif
            Test.Check("杀游戏前有 2 项开着",
                Test.WaitFor(() => { screen = Screen(trainer.Id); return Count(screen, "[x]") == 2; }, 3000),
                "on=" + Count(screen, "[x]"));

            Test.Stop(fake);

            bool reset = Test.WaitFor(() =>
            {
                screen = Screen(trainer.Id);
                return Has(screen, "未检测到游戏进程") && Count(screen, "[x]") == 0 && Count(screen, "[ ]") == Total;
            }, 8000);
            Test.Check("断开后显示未检测到游戏进程", Has(screen, "未检测到游戏进程"), "screen=" + Dump(screen));
            Test.Check("断开后 " + Total + " 个开关全部复位", reset && Count(screen, "[x]") == 0 && Count(screen, "[ ]") == Total,
                "on=" + Count(screen, "[x]") + " off=" + Count(screen, "[ ]") + " screen=" + Dump(screen));

            // ---- 断开期间按热键 ----
            int before = CountCmds();
            for (int i = 0; i < 10; i++) Tap(0x61, false);
            Thread.Sleep(400);
            Test.Check("断开期间按 10 次热键，一条都不落地", CountCmds() == before,
                "before=" + before + " after=" + CountCmds());
            Test.Check("断开期间修改器没崩", !trainer.HasExited, "exited");
            Test.Check("断开期间界面保持全关",
                Test.WaitFor(() => { screen = Screen(trainer.Id); return Count(screen, "[x]") == 0; }, 2000),
                "on=" + Count(screen, "[x]"));

            // ---- 重启游戏：自动重连 ----
            Process fake2 = Test.StartFake(_logPath, 0);
            Test.Check("重启游戏后自动重连",
                Test.WaitFor(() => { screen = Screen(trainer.Id); return Has(screen, "已连接"); }, 10000),
                "screen=" + Dump(screen));
            Test.Check("重连后开关仍全关（不自动恢复）", Count(screen, "[x]") == 0 && Count(screen, "[ ]") == Total,
                "on=" + Count(screen, "[x]"));

            int b2 = CountCmds();
            Tap(0x62, false);
            Test.Check("重连后热键重新生效", Test.WaitFor(() => CountCmds() == b2 + 1, 4000),
                "before=" + b2 + " after=" + CountCmds() + " cmds=" + string.Join(" | ", CmdLines().ToArray()));

            // ---- 杀掉修改器（模拟关窗口）----
            try { trainer.Kill(); trainer.WaitForExit(3000); } catch { }
            trainer.Dispose();
            Thread.Sleep(800);
            Test.Check("修改器被杀后 DLL 侧能感知断开", Test.ReadAll(_logPath).Contains("DISCONNECTED"),
                "log=" + Test.ReadAll(_logPath));

            Test.Stop(fake2);
            Test.KillFakes();
            Thread.Sleep(200);
        }
    }
}
