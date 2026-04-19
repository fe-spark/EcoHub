"use client";

import { useRouter } from "next/navigation";
import { Button } from "antd";
import { CaretRightOutlined, RocketOutlined } from "@ant-design/icons";
import FilmList from "@/components/public/FilmList";
import { readHistoryMap } from "@/lib/historyStorage";
import { useAppMessage } from "@/lib/useAppMessage";
import { FALLBACK_IMG } from "@/lib/fallbackImg";
import styles from "./index.module.less";

export default function FilmDetailPageView({
  data,
  link,
}: {
  data: any;
  link: string;
}) {
  const router = useRouter();
  const { message } = useAppMessage();

  const { detail, relate } = data;
  const playableSource =
    detail?.list?.find((item: any) => item?.id && item?.linkList?.length > 0) ??
    detail?.list?.find((item: any) => item?.id);

  const handlePlayClick = () => {
    if (!detail?.id) {
      message.error("影片信息不完整，暂时无法播放");
      return;
    }

    const historyMap = readHistoryMap();
    const savedState = historyMap[detail.id];

    if (savedState && savedState.link && savedState.link.includes("/play")) {
      router.push(savedState.link);
      return;
    }

    if (!playableSource?.id) {
      message.error("当前影片暂无可用播放源");
      return;
    }

    router.push(`/play?id=${link}&source=${playableSource.id}&episode=0`);
  };

  return (
    <div className={styles.container}>
      <div className={styles.bgWrapper}>
        <div className={styles.bgPoster} style={{ backgroundImage: `url(${detail.picture})` }} />
        <div className={styles.bgMask} />
      </div>

      <div className={styles.content}>
        <div className={styles.left}>
          {/* 这里的封面地址来自后端动态资源，当前不引入 next/image 远程白名单配置 */}
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img
            src={detail.picture || FALLBACK_IMG}
            className={styles.poster}
            alt={detail.name}
          />
        </div>

        <div className={styles.right}>
          <h1 className={styles.title}>{detail.name}</h1>

          <div className={styles.meta}>
            {detail.descriptor.cName && (
              <span className={styles.metaItem}>{detail.descriptor.cName}</span>
            )}
            {detail.descriptor.classTag
              ?.split(",")
              .filter((tag: string) => tag.trim())
              .map((tag: string, index: number) => (
                <span key={index} className={styles.metaItem}>
                  {tag}
                </span>
              ))}
            {detail.descriptor.year && (
              <span className={styles.metaItem}>{detail.descriptor.year}</span>
            )}
            {detail.descriptor.area && (
              <span className={styles.metaItem}>{detail.descriptor.area}</span>
            )}
          </div>

          <div className={styles.actions}>
            <Button
              type="primary"
              className={styles.playBtn}
              icon={<CaretRightOutlined />}
              onClick={handlePlayClick}
            >
              立即播放
            </Button>
            <Button
              className={styles.collectBtn}
              icon={<RocketOutlined />}
              onClick={() => message.info("功能开发中...")}
            >
              收藏
            </Button>
          </div>

          <div className={styles.intro}>
            <h2 className={styles.sectionTitle}>简介</h2>
            <div className={styles.descContent}>{detail.descriptor.content || "暂无简介"}</div>
          </div>
        </div>
      </div>

      <div className={styles.recommendation}>
        <h2 className={styles.sectionTitle} style={{ marginBottom: 24 }}>
          相关推荐
        </h2>
        <FilmList list={relate} className={styles.classifyGrid} />
      </div>
    </div>
  );
}
