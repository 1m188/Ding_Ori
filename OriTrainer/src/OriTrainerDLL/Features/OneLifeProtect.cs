using System;
using System.Threading;

namespace OriTrainerDLL.Features
{
    // 一命保护（死亡不清档）：把 DifficultyController.Difficulty 保持为 Easy，
    // 让"一命死亡即清档"那条链整条不成立，死亡退化为普通模式的检查点复活。
    //
    // ---- 一命清档是怎么发生的 ----
    // SeinDamageReciever.OnKill（IL）：
    //     IL_009C  GameScheduler.OnPlayerDeath.Call()
    //     IL_00AB  if (DifficultyController.Difficulty != OneLife) goto IL_00DF;  ← 判据
    //     IL_00BB      SaveSlotInfo.WasKilled = true
    //     IL_00D0      SaveGameController.PerformSave()
    //     IL_00DA      SaveSlotBackupsManager.DeleteAllBackups()   ← File::Delete ×5
    // 拦掉 IL_00AB 那道 if，后面三件事全部不执行。其中真正不可逆的只有
    // DeleteAllBackups（真删磁盘上的 5 个备份文件），所以这是本功能存在的意义。
    //
    // ⚠ 判据读的是 DifficultyController.Difficulty，**不是** LowestDifficulty
    //   （get_SaveWasOneLifeAndKilled 才是读 SaveSlotInfo.Difficulty）。
    //   本功能只动 Difficulty。
    //
    // ---- 绝不碰 LowestDifficulty ----
    // 成就是否算"一命通关"只读 DifficultyController.LowestDifficulty：
    //     AchievementsLogic+<OnAct3EndIEnumerator>c__Iterator6.MoveNext:
    //         ldfld DifficultyController::LowestDifficulty  → switch
    //             case 3 (OneLife): AwardAchievement(BeatOneLifeAchievementAsset)
    // 写它会毁掉成就资格。它在 ChangeDifficulty 里用 Mathf.Min 单向下降，我们不碰，
    // 所以成就照拿 —— 这正是本功能与"做成就"目的的契合点。
    //
    // ---- 只在"一命存档"下介入 ----
    // 先读 LowestDifficulty 确认这是一命存档（OneLife == 3），普通/困难存档直接不动，
    // 避免改动玩家本来选的难度。
    //
    // ---- 为什么选 Easy ----
    // 本功能的用途是方便拿成就，顺手把难度降下来是有意为之，不是副作用。
    // Easy 相对一命/普通在各消费点的实测差异（逐个核过 IL）：
    //   · SeinDamageReciever.OnRecieveDamage：switch 只有 Easy/Normal/Hard 三个 case，
    //     OneLife 落默认分支。Easy 下 Amount/2，类型 1/3 走 (cells-3)*0.5+3。
    //   · Enemy.ScaleHealth：Ceil(Amount * 0.65)。⚠ 只在 15 个 *EnemyPlaceholder::Instantiate
    //     里调用，即生成时算一次，场上已有的敌人不会追溯变化。
    //   · RisingWater.FixedUpdate：走 EasySpeedOverDistance 曲线（涨水更慢）。
    //   · SelfDestruct.FixedUpdate：HalfTimeOnEasy 且 Easy 时每帧扣两次 deltaTime，
    //     即自爆计时减半 —— 这是唯一一处 Easy 反而更难的地方。
    //   · OrbSpawner / SkillItem **只特判 Hard（==2）**，Easy 与 OneLife 相同，不受影响。
    // 另外 CanChangeDifficultyCondition.Validate 是 `Difficulty != OneLife` 才 true，
    // 所以改成 Easy 后游戏内"更改难度"选项会重新可用，玩家可以手动改掉。
    //
    // ---- 为什么必须持续写入 ----
    // DifficultyController 继承 SaveSerialize，Serialize 里 ar.Serialize 双向读写
    // Difficulty 与 LowestDifficulty（即存档字段），而：
    //   · 读档：SaveGameController.RestoreCheckpointPart1 → SaveSceneManager.Load
    //   · 死亡复活：RestoreCheckpointController.RestoreCheckpoint → SaveSceneManager.Load
    // 两条路径都会走 SeinWorldState/DifficultyController 的 Serialize 反序列化，
    // 把落盘的旧值读回来。另外 GameController.RestartGame / RestartOneLifeMode 会触发
    // GameScheduler.OnGameReset，而 DifficultyController.OnGameReset 是
    // `Difficulty = 1; LowestDifficulty = 1;`（两个都重置成 Normal）。
    // 所以写一次挡不住，必须持续压制。写了守卫是因为正常游玩时它几乎恒为 Easy，
    // 写入次数 ≈ 被回滚的次数，而非每秒 100 次（与 ZeroDeaths 同理）。
    //
    // ---- 为什么不需要主线程钩子 ----
    // 只是写一个 enum 字段（底层就是 int），且读的是 UnityEngine.Object 的 Instance 判空，
    // 走 op_Equality → CompareBaseObjects → IsNativeObjectAlive → GetCachedPtr，全是托管 IL。
    // 因此与 ZeroDeaths / CompleteExploration 一样用定时器即可。
    //
    // ---- 已知连带效果（不可逆）----
    //   · SaveSlotInfo.FillData（只在 SaveGameController.PerformSave 里调）会把
    //     DifficultyController.Difficulty 拷进 SaveSlotInfo.Difficulty 并落盘。
    //     所以开启期间只要存过一次档，该槽的 SaveSlotInfo.Difficulty 就永久不再是
    //     OneLife，存档界面显示的难度随之改变（SaveSlotUI.Apply / SaveSlotsUI.CopySaveSlots
    //     都读它）。get_SaveWasOneLifeAndKilled 也因此不再返回 true。
    //     ⚠ 但成就不受影响（它读的是 LowestDifficulty）。
    //   · AchievementsController.AwardAchievement 的 CheatsHandler.DebugWasEnabled 门控
    //     与本功能无关（只要不开官方调试菜单）。
    //
    // ---- 停止不还原 ----
    // Stop() 只停定时器，不回写难度：Difficulty 是存档字段，开启期间游戏存过档就已经
    // 落盘，还原只会给出"能收回来"的假象。与 UnlockAllAbilities / GrantKeys 同理。
    public static class OneLifeProtect
    {
        private const int IntervalMs = 10;

        private static Timer _timer;

        public static void Start()
        {
            if (_timer != null) return; // 幂等：重复 Start 不重复起定时器

            _timer = new Timer(Tick, null, 0, IntervalMs);
        }

        public static void Stop()
        {
            if (_timer == null) return;

            _timer.Dispose();
            _timer = null;
        }

        private static void Tick(object state)
        {
            // 定时器回调里的未捕获异常会终止整个进程（即游戏），必须自己兜住
            try
            {
                // 未进游戏、或对象已销毁时为 null。DifficultyController 是 MonoBehaviour，
                // 这里的 != null 走 UnityEngine.Object::op_Equality，假空（原生对象已销毁
                // 但托管引用还在）同样会被判为 null，不必自己判 m_CachedPtr。
                DifficultyController controller = DifficultyController.Instance;
                if (controller == null) return;

                // 只在"一命存档"下介入：普通/困难存档不动，避免改动玩家选的难度。
                // 注意这里读的是 LowestDifficulty（历史最低），它对一命存档恒为 OneLife。
                if (controller.LowestDifficulty != DifficultyMode.OneLife) return;

                // 判据字段：改成 Easy 后 OnKill 里那道 `!= OneLife` 就不成立，清档整块跳过。
                if (controller.Difficulty != DifficultyMode.Easy)
                    controller.Difficulty = DifficultyMode.Easy;
            }
            catch { }
        }
    }
}
