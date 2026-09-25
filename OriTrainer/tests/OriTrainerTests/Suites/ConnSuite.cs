// 连接状态机：直接调用真实的 PipeClient / Status，用假 oriDE.exe 当对端，
// 断言状态迁移与命令流。
//
// 本套件不碰真游戏、不碰 DLL —— 注入一段必然失败（假游戏没有 mono.dll），
// 正好用来验证"注入失败不放弃、下一轮继续"这条。
//
// 移植自开发期脚手架，断言逐条保留（含每条的理由注释）。

using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.Threading;
using OriTrainerDE;

namespace OriTrainerTests
{
    internal static class ConnSuite
    {
        private static string _logPath;

        private static int CountCmds(string cmd)
        {
            int n = 0;
            foreach (string l in Test.ReadAll(_logPath).Split('\n'))
                if (l.Contains("CMD " + cmd)) n++;
            return n;
        }

        private static int CountAllCmds()
        {
            int n = 0;
            foreach (string l in Test.ReadAll(_logPath).Split('\n'))
                if (l.Contains("CMD ")) n++;
            return n;
        }

        // 按顺序取出所有命令（去掉 "CMD " 前缀与尾部空白）
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
            Test.Section("连接状态机");

            _logPath = Path.Combine(Test.Dir, "conn_" + Process.GetCurrentProcess().Id + ".log");

            Test.KillFakes();
            Thread.Sleep(200);
            if (File.Exists(_logPath)) File.Delete(_logPath);

            PipeClient.Start();

            // ---- 1. 无游戏 ----
            Test.Check("无游戏时状态为 NoGame",
                Test.WaitFor(() => Status.Connection == ConnectionState.NoGame, 1500),
                "actual=" + Status.Connection);
            Test.Check("无游戏时 Pid 为 0", Status.Pid == 0, "actual=" + Status.Pid);
            Test.Check("无游戏时 TrySend 返回 false", PipeClient.TrySend("UnlimitedLife Start") == false, "expected false");

            // ---- 2. 后开游戏自动连上 ----
            Process fake = Test.StartFake(_logPath, 0);
            Test.Check("后开游戏自动连上",
                Test.WaitFor(() => Status.Connection == ConnectionState.Connected, 3000),
                "actual=" + Status.Connection + " pid=" + Status.Pid);
            Test.Check("Pid 记录为假游戏", Status.Pid == fake.Id,
                "actual=" + Status.Pid + " expected=" + fake.Id);
            Test.Check("连上后 TrySend 返回 true", PipeClient.TrySend("ShowMap Start"), "expected true");
            Test.Check("假游戏收到该命令",
                Test.WaitFor(() => CountCmds("ShowMap Start") == 1, 1500),
                "count=" + CountCmds("ShowMap Start"));

            // ---- 3. 连发多条 ----
            PipeClient.TrySend("SuperJump Stop");
            PipeClient.TrySend("GrantKeys Start");
            Test.Check("连发命令全部送达", Test.WaitFor(() => CountAllCmds() == 3, 1500),
                "count=" + CountAllCmds());
            Test.Check("命令顺序与发送顺序一致",
                CmdsAre("ShowMap Start", "SuperJump Stop", "GrantKeys Start"),
                "cmds=" + string.Join(" | ", CmdLines().ToArray()));

            // ---- 4. 杀掉游戏 → 断开 ----
            Test.Stop(fake);
            Test.Check("杀游戏后回到 NoGame",
                Test.WaitFor(() => Status.Connection == ConnectionState.NoGame, 3000),
                "actual=" + Status.Connection);
            Test.Check("断开后 Pid 归零", Test.WaitFor(() => Status.Pid == 0, 1500),
                "actual=" + Status.Pid);
            Test.Check("断开后 TrySend 返回 false", PipeClient.TrySend("UnlimitedLife Start") == false, "expected false");

            int before = CountAllCmds();
            for (int i = 0; i < 20; i++) PipeClient.TrySend("UnlimitedLife Start");
            Thread.Sleep(300);
            Test.Check("断开期间发 20 条，一条都不落地", CountAllCmds() == before,
                "before=" + before + " after=" + CountAllCmds());

            // ---- 5. 重启游戏 → 自动重连到新 PID ----
            Process fake2 = Test.StartFake(_logPath, 0);
            Test.Check("重启后自动重连",
                Test.WaitFor(() => Status.Connection == ConnectionState.Connected, 3000),
                "actual=" + Status.Connection);
            Test.Check("PID 更新为新进程", Status.Pid == fake2.Id,
                "actual=" + Status.Pid + " expected=" + fake2.Id);
            Test.Check("重连后命令可达",
                PipeClient.TrySend("ZeroDeaths Start") && Test.WaitFor(() => CountCmds("ZeroDeaths Start") == 1, 1500),
                "count=" + CountCmds("ZeroDeaths Start"));

            // ---- 6. 已连接期间不产生额外命令 ----
            int tryBefore = CountAllCmds();
            Thread.Sleep(600);
            Test.Check("已连接期间不产生额外命令", CountAllCmds() == tryBefore, "changed");
            Test.Check("已连接期间状态稳定为 Connected", Status.Connection == ConnectionState.Connected,
                "actual=" + Status.Connection);

            // ---- 7. 管道存在时根本不查就绪门，直接连上 ----
            //
            // 就绪门只在【管道不存在、即将注入】那条路径上生效。这个假游戏每次都建管道，
            // 所以它连 TryConnect 就成功了、门根本不会被问到 —— 这条断言的意义在于
            // 确认门没有误伤正常连接路径（门写错了很容易变成"永远连不上"）。
            Test.Stop(fake2);
            Thread.Sleep(300);
            Process fake3 = Test.StartFake(_logPath, 0);
            Test.Check("管道存在时直接连上（不经过就绪门）",
                Test.WaitFor(() => Status.Connection == ConnectionState.Connected, 5000),
                "actual=" + Status.Connection);
            Test.Check("连上后可发命令", PipeClient.TrySend("ShowMap Stop"), "expected true");

            Test.Stop(fake3);
            Thread.Sleep(300);

            PipeClient.Stop();
        }
    }
}
