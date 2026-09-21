using System;
using System.IO;
using System.Reflection;
using SharpMonoInjector;

namespace OriTrainerDE
{
    internal static class Program
    {
        // 游戏进程名称
        private static readonly string processName = "oriDE";

        private static void Main()
        {
            // 载荷由 csproj 内嵌进本 exe，资源名见 OriTrainerDE.csproj 的 LogicalName
            byte[] payload;
            using (Stream s = Assembly.GetExecutingAssembly()
                       .GetManifestResourceStream("OriTrainerDEDLL.dll"))
            {
                payload = new byte[s.Length];
                s.Read(payload, 0, payload.Length); // 读取载荷字节
            }

            Console.WriteLine("载荷 {0} 字节", payload.Length);

            using (Injector injector = new Injector(processName))
            {
                injector.Inject(payload, "OriTrainerDEDLL", "Program", "Load");
            }
            Console.WriteLine("注入成功");
        }
    }
}
