#include <QApplication>
#include <QGuiApplication>
#include <QScreen>
#include <QEvent>
#include <QKeyEvent>
#include <QMouseEvent>
#include <QCloseEvent>
#include <QIcon>
#include <QImage>
#include <QPixmap>
#include <QWidget>
#include <QVBoxLayout>
#include <QHBoxLayout>
#include <QLineEdit>
#include <QPushButton>
#include <QListWidget>
#include <QListWidgetItem>
#include <QLabel>
#include <QMenu>
#include <QAction>
#include <QHash>
#include <QSet>
#include <QDBusConnection>
#include <QDBusMessage>
#include <QDBusArgument>
#include <QDBusVariant>
#include <QDBusReply>
#include <QtEndian>
#include <sys/socket.h>
#include <sys/time.h>
#include <sys/un.h>
#include <unistd.h>
#include <fcntl.h>
#include <poll.h>
#include <errno.h>
#include <atomic>
#include <cstdlib>
#include <QJsonDocument>
#include <QJsonObject>
#include <QJsonArray>
#include <QJsonValue>
#include <QVariant>
#include <QStringList>
#include <QSocketNotifier>
#include <QTimer>
#include <cstdlib>
#include <cstring>

static QString socketPath() {
    const char *e = std::getenv("TRAY_SOCKET");
    if (e && *e) return QString::fromUtf8(e);
    const char *xdg = std::getenv("XDG_RUNTIME_DIR");
    QString base = xdg ? QString::fromUtf8(xdg) : QString("/tmp");
    return base + "/tray.sock";
}

static QByteArray sendCmd(const QJsonObject &req) {
    int fd = ::socket(AF_UNIX, SOCK_STREAM, 0);
    if (fd < 0) return QByteArray();
    struct timeval tv;
    tv.tv_sec = 2;
    tv.tv_usec = 0;
    ::setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &tv, sizeof(tv));
    ::setsockopt(fd, SOL_SOCKET, SO_SNDTIMEO, &tv, sizeof(tv));

    struct sockaddr_un addr;
    memset(&addr, 0, sizeof(addr));
    addr.sun_family = AF_UNIX;
    QByteArray path = socketPath().toUtf8();
    if (path.size() >= (int)sizeof(addr.sun_path)) { ::close(fd); return QByteArray(); }
    strncpy(addr.sun_path, path.constData(), sizeof(addr.sun_path)-1);
    if (::connect(fd, (struct sockaddr*)&addr, sizeof(addr)) < 0) { ::close(fd); return QByteArray(); }

    QByteArray payload = QJsonDocument(req).toJson(QJsonDocument::Compact) + "\n";
    const char *payloadData = payload.constData();
    int payloadSize = payload.size();
    int sentTotal = 0;
    while (sentTotal < payloadSize) {
#ifdef MSG_NOSIGNAL
        ssize_t n = ::send(fd, payloadData + sentTotal, payloadSize - sentTotal, MSG_NOSIGNAL);
#else
        ssize_t n = ::send(fd, payloadData + sentTotal, payloadSize - sentTotal, 0);
#endif
        if (n <= 0) { ::close(fd); return QByteArray(); }
        sentTotal += (int)n;
    }

    QByteArray data;
    char buf[4096];
    const int maxResponse = 1024 * 1024;
    while (data.size() < maxResponse) {
        ssize_t n = ::recv(fd, buf, sizeof(buf), 0);
        if (n < 0 && (errno == EINTR)) continue;
        if (n <= 0) break;
        data.append(buf, (int)n);
        if (data.contains('\n')) break;
    }
    ::close(fd);
    int nl = data.indexOf('\n');
    if (nl >= 0) {
        data = data.left(nl + 1);
    } else if (data.size() >= maxResponse) {
        return QByteArray();
    }
    return data;
}

static QJsonObject parseObj(const QByteArray &data) {
    QJsonParseError err;
    QJsonDocument doc = QJsonDocument::fromJson(data, &err);
    if (doc.isObject()) return doc.object();
    return QJsonObject();
}

struct PixmapCacheEntry {
    QString key;
    QIcon icon;
};

static QHash<QString, PixmapCacheEntry> g_pixmapCache;

static QString pixmapCacheKey(const QString &service, const QString &path, const QString &iconName, int rev) {
    return service + "\n" + path + "\n" + iconName + "\n" + QString::number(rev);
}

static QIcon iconFromSniPixmap(const QString &service, const QString &path) {
    QDBusMessage msg = QDBusMessage::createMethodCall(
        service, path,
        "org.freedesktop.DBus.Properties", "Get");
    msg << QString("org.kde.StatusNotifierItem") << QString("IconPixmap");
    QDBusMessage reply = QDBusConnection::sessionBus().call(msg, QDBus::Block, 100);
    if (reply.type() != QDBusMessage::ReplyMessage || reply.arguments().isEmpty())
        return QIcon();

    QDBusVariant dbusVariant = qvariant_cast<QDBusVariant>(reply.arguments().at(0));
    const QDBusArgument arg = qvariant_cast<QDBusArgument>(dbusVariant.variant());
    if (arg.currentType() != QDBusArgument::ArrayType)
        return QIcon();

    QIcon icon;
    arg.beginArray();
    while (!arg.atEnd()) {
        arg.beginStructure();
        int w = 0, h = 0;
        QByteArray data;
        arg >> w >> h >> data;
        arg.endStructure();
        if (w <= 0 || h <= 0 || w > 256 || h > 256)
            continue;
        if (data.size() != w * h * 4)
            continue;
        QImage img(w, h, QImage::Format_ARGB32);
        bool anyOpaque = false;
        for (int y = 0; y < h; ++y) {
            QRgb *line = reinterpret_cast<QRgb*>(img.scanLine(y));
            const uchar *src = reinterpret_cast<const uchar*>(data.constData()) + y * w * 4;
            for (int x = 0; x < w; ++x) {
                const quint32 px = qFromBigEndian<quint32>(src + x * 4);
                line[x] = static_cast<QRgb>(px);
                if ((px >> 24) != 0)
                    anyOpaque = true;
            }
        }
        if (anyOpaque)
            icon.addPixmap(QPixmap::fromImage(img));
    }
    arg.endArray();
    return icon;
}

static QIcon iconForItem(const QString &service, const QString &path, const QString &iconName, int rev) {
    const QString key = pixmapCacheKey(service, path, iconName, rev);
    auto it = g_pixmapCache.constFind(key);
    if (it != g_pixmapCache.constEnd())
        return it->icon;

    QIcon ic;
    if (!iconName.isEmpty()) {
        ic = QIcon::fromTheme(iconName);
    }
    if (ic.isNull()) {
        ic = iconFromSniPixmap(service, path);
    }
    if (ic.isNull()) {
        ic = QIcon::fromTheme("application-x-executable");
    }
    g_pixmapCache.insert(key, {key, ic});
    return ic;
}

static void addMenuActions(QMenu *menu, const QJsonArray &entries, const QString &service, const QString &menuPath);

static void buildMenu(QMenu *menu, const QJsonArray &entries, const QString &service, const QString &menuPath) {
    for (const QJsonValue &v : entries) {
        QJsonObject e = v.toObject();
        int id = e["id"].toInt();
        QString type = e["type"].toString();
        if (type == "separator") {
            menu->addSeparator();
            continue;
        }
        QString label = e["label"].toString();
        bool enabled = e.contains("enabled") ? e["enabled"].toBool() : true;
        QString toggleType = e["toggleType"].toString();
        int toggleState = e.contains("toggleState") ? e["toggleState"].toInt() : -1;
        QJsonArray children = e["children"].toArray();
        if (label.isEmpty()) {
            if (!children.isEmpty()) {
                auto *sub = new QMenu(menu);
                buildMenu(sub, children, service, menuPath);
                auto *act = new QAction("(submenu)", menu);
                act->setMenu(sub);
                menu->addAction(act);
            }
            continue;
        }
        QString displayLabel = label;
        displayLabel.replace("&", "&&");
        auto *act = new QAction(displayLabel, menu);
        act->setEnabled(enabled);
        if (toggleType == "checkmark" || toggleType == "radio") {
            act->setCheckable(true);
            act->setChecked(toggleState == 1);
        }
        if (!children.isEmpty()) {
            auto *sub = new QMenu(menu);
            buildMenu(sub, children, service, menuPath);
            act->setMenu(sub);
        } else {
            QObject::connect(act, &QAction::triggered, [service, menuPath, id]() {
                QJsonObject req;
                req["cmd"] = "menuevent";
                req["service"] = service;
                req["menu"] = menuPath;
                req["id"] = id;
                sendCmd(req);
            });
        }
        menu->addAction(act);
    }
}

static QWidget *g_window = nullptr;
static std::atomic<bool> g_quitRequested{false};

class Window : public QWidget {
public:
    Window(QWidget *parent=nullptr) : QWidget(parent), watchFd(-1), watchNotifier(nullptr) {
        g_window = this;
        QApplication::setQuitOnLastWindowClosed(false);
        setWindowTitle("Tray");
        setWindowFlags(Qt::Tool | Qt::FramelessWindowHint | Qt::WindowStaysOnTopHint);
        resize(360, 460);
        // niri auto-floats windows whose minimum and maximum sizes are equal.
        setMinimumSize(360, 460);
        setMaximumSize(360, 460);
        if (QScreen *screen = QGuiApplication::primaryScreen()) {
            move(screen->availableGeometry().center() - rect().center());
        }
        auto *layout = new QVBoxLayout(this);
        auto *top = new QHBoxLayout();
        search = new QLineEdit(this);
        search->setPlaceholderText("Filter...");
        refreshBtn = new QPushButton("Refresh", this);
        top->addWidget(search);
        top->addWidget(refreshBtn);
        layout->addLayout(top);
        list = new QListWidget(this);
        layout->addWidget(list);
        status = new QLabel("Ready", this);
        layout->addWidget(status);

        connect(refreshBtn, &QPushButton::clicked, this, &Window::refresh);
        connect(search, &QLineEdit::textChanged, this, &Window::applyFilter);
        connect(list, &QListWidget::itemActivated, this, &Window::activateItem);

        qApp->installEventFilter(this);
        list->viewport()->installEventFilter(this);

        list->setContextMenuPolicy(Qt::CustomContextMenu);
        connect(list, &QWidget::customContextMenuRequested, this, &Window::contextMenu);

        // Initial load
        refresh();

        // Start watching for pushed snapshots
        connectWatch();
    }

    ~Window() {
        if (watchNotifier) {
            watchNotifier->setEnabled(false);
            delete watchNotifier;
        }
        if (watchFd >= 0) {
            ::close(watchFd);
        }
    }

private:
    void connectWatch() {
        if (watchNotifier) {
            watchNotifier->setEnabled(false);
            watchNotifier->deleteLater();
            watchNotifier = nullptr;
        }
        if (watchFd >= 0) {
            ::close(watchFd);
            watchFd = -1;
        }
        watchBuf.clear();

        watchFd = ::socket(AF_UNIX, SOCK_STREAM, 0);
        if (watchFd < 0) {
            QTimer::singleShot(1000, this, &Window::connectWatch);
            return;
        }
        struct timeval tv;
        tv.tv_sec = 2;
        tv.tv_usec = 0;
        ::setsockopt(watchFd, SOL_SOCKET, SO_SNDTIMEO, &tv, sizeof(tv));
        struct sockaddr_un addr;
        memset(&addr, 0, sizeof(addr));
        addr.sun_family = AF_UNIX;
        QByteArray path = socketPath().toUtf8();
        if (path.size() >= (int)sizeof(addr.sun_path)) {
            ::close(watchFd);
            watchFd = -1;
            QTimer::singleShot(1000, this, &Window::connectWatch);
            return;
        }
        strncpy(addr.sun_path, path.constData(), sizeof(addr.sun_path)-1);
        if (::connect(watchFd, (struct sockaddr*)&addr, sizeof(addr)) < 0) {
            ::close(watchFd);
            watchFd = -1;
            QTimer::singleShot(1000, this, &Window::connectWatch);
            return;
        }

        QJsonObject req;
        req["cmd"] = "watch";
        QByteArray payload = QJsonDocument(req).toJson(QJsonDocument::Compact) + "\n";
        const char *p = payload.constData();
        int ps = payload.size();
        int sentTotal = 0;
        while (sentTotal < ps) {
#ifdef MSG_NOSIGNAL
            ssize_t n = ::send(watchFd, p + sentTotal, ps - sentTotal, MSG_NOSIGNAL);
#else
            ssize_t n = ::send(watchFd, p + sentTotal, ps - sentTotal, 0);
#endif
            if (n < 0 && errno == EINTR) continue;
            if (n <= 0) {
                ::close(watchFd);
                watchFd = -1;
                QTimer::singleShot(1000, this, &Window::connectWatch);
                return;
            }
            sentTotal += (int)n;
        }

        int flags = fcntl(watchFd, F_GETFL, 0);
        if (flags >= 0) fcntl(watchFd, F_SETFL, flags | O_NONBLOCK);

        watchNotifier = new QSocketNotifier(watchFd, QSocketNotifier::Read, this);
        connect(watchNotifier, &QSocketNotifier::activated, this, &Window::onWatchData);
    }

    void onWatchData(int) {
        if (watchFd < 0) return;
        char buf[4096];
        while (true) {
            ssize_t n = ::recv(watchFd, buf, sizeof(buf), 0);
            if (n < 0 && (errno == EAGAIN || errno == EWOULDBLOCK)) {
                break;
            }
            if (n < 0 && errno == EINTR) {
                continue;
            }
            if (n <= 0) {
                if (watchNotifier) {
                    watchNotifier->setEnabled(false);
                    watchNotifier->deleteLater();
                    watchNotifier = nullptr;
                }
                if (watchFd >= 0) {
                    ::close(watchFd);
                    watchFd = -1;
                }
                watchBuf.clear();
                QTimer::singleShot(1000, this, &Window::connectWatch);
                return;
            }
            watchBuf.append(buf, (int)n);
            if (watchBuf.size() > 2 * 1024 * 1024) {
                watchBuf.clear();
            }
        }
        while (true) {
            int idx = watchBuf.indexOf('\n');
            if (idx < 0) break;
            QByteArray line = watchBuf.left(idx);
            watchBuf = watchBuf.mid(idx + 1);
            line = line.trimmed();
            if (line.isEmpty()) continue;
            QJsonParseError err;
            QJsonDocument doc = QJsonDocument::fromJson(line, &err);
            if (doc.isObject()) {
                QJsonObject obj = doc.object();
                if (obj.contains("items")) {
                    currentItems = obj["items"].toArray();
                    rebuild();
                    status->setText(QString("%1 item(s)").arg(currentItems.size()));
                }
            }
        }
    }

    void refresh() {
        QJsonObject req; req["cmd"] = "list";
        QByteArray data = sendCmd(req);
        QJsonObject obj = parseObj(data);
        currentItems = obj["items"].toArray();
        rebuild();
        status->setText(QString("%1 item(s)").arg(currentItems.size()));
    }

    void rebuild() {
        QString currentKey;
        if (auto *cur = list->currentItem())
            currentKey = cur->data(Qt::UserRole).toString();
        const bool hadFocus = list->hasFocus() || list->viewport()->hasFocus();

        QString filt = search->text().toLower();
        list->clear();
        QListWidgetItem *restore = nullptr;
        QSet<QString> liveIconKeys;
        for (const QJsonValue &v : currentItems) {
            QJsonObject it = v.toObject();
            QString title = it["title"].toString();
            if (title.isEmpty()) title = "(no title)";
            QString service = it["service"].toString();
            QString path = it["path"].toString();
            QString menu = it["menu"].toString();
            QString icon = it["icon"].toString();
            int rev = it["rev"].toInt();
            int pid = it["pid"].toInt();
            QString clientTip = it["tooltip"].toString();
            liveIconKeys.insert(pixmapCacheKey(service, path, icon, rev));
            if (!filt.isEmpty() && !title.toLower().contains(filt) && !service.toLower().contains(filt)) continue;
            auto *item = new QListWidgetItem(title, list);
            item->setIcon(iconForItem(service, path, icon, rev));
            QString tip = title + "\nService: " + service + "\nPath: " + path;
            if (pid > 0) tip += "\nPID: " + QString::number(pid);
            if (!clientTip.isEmpty()) tip += "\n\n" + clientTip;
            item->setToolTip(tip);
            const QString key = service + "\x1f" + path + "\x1f" + menu;
            item->setData(Qt::UserRole, key);
            if (!currentKey.isEmpty() && key == currentKey)
                restore = item;
        }
        for (auto it = g_pixmapCache.begin(); it != g_pixmapCache.end();) {
            if (!liveIconKeys.contains(it.key()))
                it = g_pixmapCache.erase(it);
            else
                ++it;
        }
        if (restore)
            list->setCurrentItem(restore);
        if (hadFocus)
            list->setFocus();
    }

    void applyFilter(const QString &) { rebuild(); }

    void activateItem(QListWidgetItem *item) {
        if (!item) return;
        QStringList parts = item->data(Qt::UserRole).toString().split("\x1f");
        if (parts.size() < 2) return;
        QJsonObject req; req["cmd"]="activate"; req["service"]=parts[0]; req["path"]=parts[1];
        sendCmd(req);
        hide();
    }

    void closeEvent(QCloseEvent *event) override {
        hide();
        event->ignore();
    }

    bool eventFilter(QObject *obj, QEvent *event) override {
        if (event->type() == QEvent::KeyPress &&
            static_cast<QKeyEvent*>(event)->key() == Qt::Key_Escape &&
            !QApplication::activePopupWidget()) {
            hide();
            return true;
        }
        if (obj == list->viewport() && event->type() == QEvent::MouseButtonPress) {
            QMouseEvent *me = static_cast<QMouseEvent*>(event);
            if (me->button() == Qt::MiddleButton) {
                QListWidgetItem *item = list->itemAt(me->pos());
                if (item) {
                    QStringList parts = item->data(Qt::UserRole).toString().split("\x1f");
                    if (parts.size() >= 2) {
                        QJsonObject req; req["cmd"]="secondary"; req["service"]=parts[0]; req["path"]=parts[1];
                        sendCmd(req);
                        hide();
                    }
                    return true;
                }
            }
        }
        return QWidget::eventFilter(obj, event);
    }

    void contextMenu(const QPoint &pos) {
        QListWidgetItem *item = list->itemAt(pos);
        if (!item) return;
        QStringList parts = item->data(Qt::UserRole).toString().split("\x1f");
        if (parts.size() < 3) return;
        QString service = parts[0];
        QString menuPath = parts[2];
        if (menuPath.isEmpty()) return;
        QJsonObject req; req["cmd"]="menu"; req["service"]=service; req["menu"]=menuPath;
        QByteArray data = sendCmd(req);
        QJsonObject obj = parseObj(data);
        QJsonArray entries = obj["items"].toArray();
        QMenu menu(this);
        buildMenu(&menu, entries, service, menuPath);
        menu.exec(list->mapToGlobal(pos));
    }

private:
    QLineEdit *search;
    QPushButton *refreshBtn;
    QListWidget *list;
    QLabel *status;
    QJsonArray currentItems;
    int watchFd;
    QSocketNotifier *watchNotifier;
    QByteArray watchBuf;
};

extern "C" void runQtUI() {
    static int argc = 0;
    static char **argv = nullptr;
    QApplication app(argc, argv);
    QGuiApplication::setApplicationName("tray");
    Window w;
    if (g_quitRequested.load()) {
        QTimer::singleShot(0, &app, &QApplication::quit);
    }
    const char *hidden = std::getenv("TRAY_UI_HIDDEN");
    const bool startHidden = hidden && hidden[0] == '1' && hidden[1] == '\0';
    if (!startHidden) {
        w.show();
    }
    if (g_quitRequested.load()) {
        QTimer::singleShot(0, &app, &QApplication::quit);
    }
    app.exec();
}

extern "C" void uiQuit() {
    g_quitRequested.store(true);
    if (qApp) {
        QMetaObject::invokeMethod(qApp, []() {
            if (qApp) qApp->quit();
        }, Qt::QueuedConnection);
    }
}

extern "C" void uiShow() {
    QWidget *w = g_window;
    if (w) {
        QMetaObject::invokeMethod(w, [w](){ w->show(); w->raise(); w->activateWindow(); }, Qt::QueuedConnection);
    }
}

extern "C" void uiHide() {
    QWidget *w = g_window;
    if (w) {
        QMetaObject::invokeMethod(w, [w](){ w->hide(); }, Qt::QueuedConnection);
    }
}

extern "C" void uiToggle() {
    QWidget *w = g_window;
    if (w) {
        QMetaObject::invokeMethod(w, [w](){
            if (w->isVisible()) w->hide();
            else { w->show(); w->raise(); w->activateWindow(); }
        }, Qt::QueuedConnection);
    }
}
