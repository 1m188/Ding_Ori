using System;
using System.Reflection;

namespace OriTrainerDLL.Features
{
    // 死亡数归零：持续把 SeinDeathCounter.m_deathCounter 保持为 0。
    //
    // ---- 为什么不写 SeinDeathCounter.Count ----
    // 游戏提供了 public static 的 Count 属性，看起来一行就能搞定：
    //     SeinDeathCounter.Count = 0;
    // 但它的 setter 带了额外动作（IL）：
    //     if (Instance != null) {
    //         Instance.m_deathCounter = value;
    //         SaveSceneManager.Master.Save(SaveGameData.Master, Instance);   // ← 白送的
    //     }
    // 这里调的是 Save(SaveScene, ISerializable) 这个【双参重载】，别和同名的单参
    // Save(SaveScene) 搞混 —— 两者代价差一个数量级：
    //   · 单参版：清空 SaveObjects 后遍历全部 ISerializable 重建整个存档缓冲（全量）；
    //   · 双参版（本条走的就是它）：SaveSerializeToId 线性查一次 id，然后只把传入的
    //     这一个对象序列化进存档，几行而已。
    // 所以「每 10ms 一次全量场景序列化」这个说法是错的，真实代价只是一次 id 线性查找
    // 加写入一个 int。
    //
    // 即便如此仍然不走属性：本功能要持续写入（见下），而属性每次都会顺手改动存档
    // 缓冲。一个"读一下、必要时清零"的功能不该有写存档这种副作用，尤其不该在游戏
    // 自己的存档流程中途插进去。直写字段则完全没有这个动作。
    //
    // 因此改为反射直写私有字段 m_deathCounter（private、非 readonly、非 static，
    // 可写）。这在语义上等同于 golang 版的裸内存写入 nav+0x14 —— 那边之所以没有
    // 副作用，正是因为它绕过了 setter；这里用元数据取得同一效果，不必自己算偏移。
    //
    // ---- 为什么必须持续写入 ----
    // SeinDamageReciever.OnKill 是唯一的自增点：
    //     SeinDeathCounter.Count = SeinDeathCounter.Count + 1;
    // 它是"读-加-写"，不是"置位"，所以写一次 0 挡不住下一次死亡。只有持续保持 0
    // 才能在整局游戏中都不累积。这与 InfiniteSkillPoints 的一次性写入不同：那个是
    // 落盘即定局，这个是必须持续压制的运行时状态。
    //
    // ---- 为什么不需要主线程钩子 ----
    // 这条链上没有任何 Unity native 调用（逐层核过 IL）：
    //     get_Count → ldsfld Instance / Object::op_Equality / ldfld m_deathCounter
    //     Object::op_Equality → CompareBaseObjects → IsNativeObjectAlive → GetCachedPtr
    // 全部是托管 IL，本来用定时器即可。但原版游戏（Unity 5.0 内置的旧 Mono 2.x）里
    // 注入 DLL 的 System.Threading.Timer 不可靠（回调不触发，实测），所以统一改挂
    // 游戏自己的每帧回调 OnGameFixedUpdate
    //
    // ---- 停止不还原 ----
    // Stop() 只注销钩子，不回写原值：m_deathCounter 是存档字段（SeinDeathCounter
    // 继承 SaveSerialize，Serialize 里就是 ar.Serialize(ref m_deathCounter)），
    // 游戏自然存档时 0 就已经落盘，还原只会给出"能收回来"的假象。
    // 停止后死亡数从当前值（0）继续正常累加。
    //
    // ---- 已知连带效果 ----
    //   · 成就：Act 3 结局时 AchievementsLogic 判定 get_Count() == 0 会授予
    //     NoDeathsAchievementAsset（"全程未死亡"）。这是本功能的预期用途。
    //   · 排行榜 / Steam 遥测：LeaderboardsController.UploadScores 与
    //     SeinDeathCounter.SendTelemetryData 读的也是这个字段，会读到 0。
    public static class ZeroDeaths
    {
        private static readonly BindingFlags Private =
            BindingFlags.NonPublic | BindingFlags.Instance;

        private static FieldInfo _fCount; // m_deathCounter
        private static Action _hook;      // 保留引用以便 Stop 时注销

        public static void Start()
        {
            if (_hook != null) return; // 幂等：重复 Start 不重复挂载

            // 字段名对不上就直接失败（Loader 会记进错误日志），而不是每帧静默空转
            _fCount = typeof(SeinDeathCounter).GetField("m_deathCounter", Private) ?? throw new Exception("SeinDeathCounter 的字段名与预期不符，功能无法工作");

            // Scheduler 由 GameController 持有，而 GameController.Awake 是单例守卫
            // （Instance 已存在则 Destroy 自身），所以该回调在整个进程内稳定可用。
            GameScheduler scheduler = Game.Events.Scheduler ?? throw new Exception("GameScheduler 尚未就绪（游戏未启动完成），功能无法挂载");

            _hook = OnGameFixedUpdate;
            scheduler.OnGameFixedUpdate.Add(_hook);
        }

        public static void Stop()
        {
            if (_hook == null) return;

            Game.Events.Scheduler.OnGameFixedUpdate.Remove(_hook);
            _hook = null;
        }

        // 由游戏主线程每个 FixedUpdate 调用
        private static void OnGameFixedUpdate()
        {
            // 游戏回调里抛出的异常会顺着 GameController.FixedUpdate 冒到 Unity，
            // 后果不可预期，必须自己兜住
            try
            {
                // 未进游戏、或对象已销毁时为 null。SeinDeathCounter 是 MonoBehaviour，
                // 这里的 != null 走 UnityEngine.Object::op_Equality，假空（原生对象已销毁
                // 但托管引用还在）同样会被判为 null，不必自己判 m_CachedPtr。
                SeinDeathCounter counter = SeinDeathCounter.Instance;
                if (counter == null) return;

                // 走 public 的 Count 读（无副作用），走反射写（绕开 setter 的存档动作）。
                // 正常游玩时写入次数 ≈ 死亡次数，而不是每秒 50 次。
                if (SeinDeathCounter.Count > 0)
                    _fCount.SetValue(counter, 0);
            }
            catch { }
        }
    }
}
