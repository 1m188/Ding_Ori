// 测试入口：按参数选择套件，便于"只跑一套"和"跑全量"。
//
//   OriTrainerTests            跑全部（默认）
//   OriTrainerTests conn       只跑连接状态机
//   OriTrainerTests gate       只跑就绪门
//   OriTrainerTests hold       只跑日志被持有
//   OriTrainerTests injectonce 只跑绝不重复注入
//   OriTrainerTests soak       只跑耐久
//   OriTrainerTests e2e        只跑端到端（需要真实 exe 已构建）
//   OriTrainerTests list       列出套件
//
// 退出码：0=全过，1=有失败。便于脚本/CI 直接判。

using System;
using System.Collections.Generic;

namespace OriTrainerTests
{
    internal static class Program
    {
        private static readonly List<Suite> All = new List<Suite>
        {
            new Suite("conn",       "连接状态机（状态迁移、命令流、自动重连）", ConnSuite.Run),
            new Suite("gate",       "就绪门（先开修改器后开游戏不注入）",       GateSuite.Run),
            new Suite("hold",       "日志被持续持有时就绪门仍工作",             HoldSuite.Run),
            new Suite("injectonce", "绝不重复注入",                             InjectOnceSuite.Run),
            new Suite("soak",       "耐久（反复重启不泄漏句柄）",               SoakSuite.Run),
            new Suite("e2e",        "端到端（真实 exe + 热键 + 界面）",         E2ESuite.Run),
        };

        private static int Main(string[] args)
        {
            string pick = args.Length > 0 ? args[0].ToLowerInvariant() : null;

            if (pick == "list")
            {
                foreach (Suite s in All) Console.WriteLine("  " + s.Name.PadRight(12) + s.Description);
                return 0;
            }

            Console.WriteLine("OriTrainer 测试" + (pick == null ? "（全量）" : "：" + pick));
            Console.WriteLine("输出目录: " + Test.Dir);

            int failed = 0;
            foreach (Suite s in All)
            {
                if (pick != null && s.Name != pick) continue;

                try
                {
                    s.Run();
                }
                catch (Exception ex)
                {
                    // 套件自己炸了也要继续跑其余套件：一次运行拿到尽可能多的信息，
                    // 否则修一个才知道下一个。
                    Console.WriteLine();
                    Console.WriteLine("!! 套件 " + s.Name + " 抛异常: " + ex);
                    failed++;
                }
            }

            Console.WriteLine();
            Console.WriteLine("总计: PASS=" + Test.Pass + "  FAIL=" + Test.Fail + "  异常套件=" + failed);
            return (Test.Fail == 0 && failed == 0) ? 0 : 1;
        }

        private sealed class Suite
        {
            public readonly string Name;
            public readonly string Description;
            public readonly Action Run;

            public Suite(string name, string description, Action run)
            {
                Name = name; Description = description; Run = run;
            }
        }
    }
}
